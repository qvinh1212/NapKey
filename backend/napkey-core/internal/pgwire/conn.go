package pgwire

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

func init() {
	sql.Register("postgres", &Driver{})
	sql.Register("pgwire", &Driver{})
}

// Driver is the database/sql driver entry point.
type Driver struct{}

// Open implements driver.Driver.
func (d *Driver) Open(dsn string) (driver.Conn, error) {
	cfg, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.connectTimeout)
	defer cancel()
	return connect(ctx, cfg)
}

// OpenConnector implements driver.DriverContext so database/sql parses the DSN
// once at sql.Open time instead of on every new connection.
func (d *Driver) OpenConnector(dsn string) (driver.Connector, error) {
	cfg, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	return &connector{cfg: cfg, driver: d}, nil
}

type connector struct {
	cfg    *dsnConfig
	driver *Driver
}

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	// Honor connect_timeout even when the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.cfg.connectTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.connectTimeout)
		defer cancel()
	}
	return connect(ctx, c.cfg)
}

func (c *connector) Driver() driver.Driver { return c.driver }

// conn is a single Postgres session.
type conn struct {
	cfg  *dsnConfig
	netC net.Conn
	r    *bufio.Reader
	w    *bufio.Writer

	// scratch backs readBuf between messages to avoid an allocation per row.
	scratch []byte

	// txStatus is the latest ReadyForQuery indicator: 'I' idle, 'T' in a
	// transaction, 'E' in a failed transaction.
	txStatus byte

	// backendPID and secretKey are needed to send a CancelRequest.
	backendPID uint32
	secretKey  uint32

	// params holds the server's reported GUCs, notably standard_conforming_strings.
	params map[string]string

	// stmtCounter names prepared statements uniquely within the session.
	stmtCounter uint64

	// closed guards against use after Close and makes Close idempotent.
	closed bool
	// bad marks a connection whose protocol state is unknown. database/sql
	// discards these rather than returning them to the pool, which is what keeps
	// one failed query from poisoning every later one.
	bad bool

	mu sync.Mutex
}

// connect dials, negotiates TLS, authenticates, and waits for ReadyForQuery.
func connect(ctx context.Context, cfg *dsnConfig) (*conn, error) {
	d := &net.Dialer{}
	netC, err := d.DialContext(ctx, "tcp", cfg.addr())
	if err != nil {
		return nil, fmt.Errorf("pgwire: dialing %s: %w", cfg.addr(), err)
	}
	// TCP keepalive surfaces a dead peer instead of leaving a pooled connection
	// that hangs on first use after a network blip.
	if tcp, ok := netC.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}

	c := &conn{
		cfg:      cfg,
		netC:     netC,
		params:   map[string]string{},
		scratch:  make([]byte, 0, 1024),
		txStatus: 'I',
	}

	// The handshake inherits the context deadline; without this a hung server
	// would block startup forever.
	if deadline, ok := ctx.Deadline(); ok {
		_ = netC.SetDeadline(deadline)
	}

	var channelBinding []byte
	if cfg.sslMode != sslDisable {
		upgraded, cb, sslErr := c.negotiateTLS(cfg)
		if sslErr != nil {
			netC.Close()
			return nil, sslErr
		}
		if upgraded {
			channelBinding = cb
		} else if cfg.sslMode != sslPrefer {
			netC.Close()
			return nil, fmt.Errorf("pgwire: server rejected TLS but sslmode=%s requires it", cfg.sslMode)
		}
	}

	c.r = bufio.NewReaderSize(c.netC, 16*1024)
	c.w = bufio.NewWriterSize(c.netC, 8*1024)

	if err := c.startup(channelBinding); err != nil {
		c.netC.Close()
		return nil, err
	}

	// Clear the handshake deadline; per-query deadlines are handled by context.
	_ = c.netC.SetDeadline(time.Time{})
	return c, nil
}

// negotiateTLS performs the SSLRequest exchange. Returns whether TLS was
// established and the channel-binding material for SCRAM-SHA-256-PLUS.
func (c *conn) negotiateTLS(cfg *dsnConfig) (bool, []byte, error) {
	// SSLRequest has no message type byte: length 8 then the magic 80877103.
	req := make([]byte, 8)
	binary.BigEndian.PutUint32(req[0:], 8)
	binary.BigEndian.PutUint32(req[4:], 80877103)
	if _, err := c.netC.Write(req); err != nil {
		return false, nil, fmt.Errorf("pgwire: sending SSLRequest: %w", err)
	}
	resp := make([]byte, 1)
	if _, err := io.ReadFull(c.netC, resp); err != nil {
		return false, nil, fmt.Errorf("pgwire: reading SSLRequest response: %w", err)
	}
	switch resp[0] {
	case 'S':
		// proceed
	case 'N':
		return false, nil, nil
	case 'E':
		return false, nil, fmt.Errorf("pgwire: server returned an error to SSLRequest (likely a pre-TLS connection limit)")
	default:
		return false, nil, fmt.Errorf("pgwire: unexpected SSLRequest response byte %q", resp[0])
	}

	tlsCfg := &tls.Config{
		ServerName: cfg.host,
		MinVersion: tls.VersionTLS12,
	}
	switch cfg.sslMode {
	case sslRequire, sslPrefer:
		// libpq semantics: require encrypts but does not authenticate the server.
		// That stops passive sniffing, not an active MITM. verify-full is the mode
		// to use across an untrusted network.
		tlsCfg.InsecureSkipVerify = true
	case sslVerifyCA:
		// Chain must validate, hostname need not match.
		tlsCfg.InsecureSkipVerify = true
		roots, err := loadRootCAs(cfg.sslRootCert)
		if err != nil {
			return false, nil, err
		}
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyChainOnly(rawCerts, roots)
		}
	case sslVerifyFull:
		if cfg.sslRootCert != "" {
			roots, err := loadRootCAs(cfg.sslRootCert)
			if err != nil {
				return false, nil, err
			}
			tlsCfg.RootCAs = roots
		}
	}

	tlsConn := tls.Client(c.netC, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		return false, nil, fmt.Errorf("pgwire: TLS handshake with %s: %w", cfg.addr(), err)
	}
	c.netC = tlsConn

	// tls-server-end-point binding is the hash of the server certificate.
	state := tlsConn.ConnectionState()
	var cb []byte
	if len(state.PeerCertificates) > 0 {
		sum := sha256.Sum256(state.PeerCertificates[0].Raw)
		cb = sum[:]
	}
	return true, cb, nil
}

func loadRootCAs(path string) (*x509.CertPool, error) {
	if path == "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("pgwire: loading system cert pool: %w", err)
		}
		return pool, nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pgwire: reading sslrootcert %s: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("pgwire: sslrootcert %s contains no valid PEM certificate", path)
	}
	return pool, nil
}

// verifyChainOnly validates the certificate chain while ignoring the hostname,
// which is what sslmode=verify-ca asks for.
func verifyChainOnly(rawCerts [][]byte, roots *x509.CertPool) error {
	if len(rawCerts) == 0 {
		return errors.New("pgwire: server presented no certificate")
	}
	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("pgwire: parsing server certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	inter := x509.NewCertPool()
	for _, cert := range certs[1:] {
		inter.AddCert(cert)
	}
	_, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter})
	if err != nil {
		return fmt.Errorf("pgwire: server certificate chain is not trusted: %w", err)
	}
	return nil
}

// startup sends StartupMessage and drives authentication to ReadyForQuery.
func (c *conn) startup(channelBinding []byte) error {
	w := &writeBuf{buf: make([]byte, 0, 128)}
	w.start(0)      // StartupMessage has no type byte.
	w.int32(196608) // protocol 3.0
	w.string("user")
	w.string(c.cfg.user)
	w.string("database")
	w.string(c.cfg.database)
	w.string("application_name")
	w.string(c.cfg.appName)
	// Predictable text encoding regardless of server locale.
	w.string("client_encoding")
	w.string("UTF8")
	// Timestamps come back in a form time.Parse handles without guessing.
	w.string("DateStyle")
	w.string("ISO, MDY")
	for k, v := range c.cfg.runtimeParams {
		w.string(k)
		w.string(v)
	}
	w.byte(0)
	if _, err := c.w.Write(w.finish()); err != nil {
		return fmt.Errorf("pgwire: sending startup message: %w", err)
	}
	if err := c.w.Flush(); err != nil {
		return fmt.Errorf("pgwire: flushing startup message: %w", err)
	}

	var scram *scramClient
	for {
		typ, body, err := c.readMessage()
		if err != nil {
			return err
		}
		switch typ {
		case 'R': // Authentication*
			code := body.int32()
			if body.err != nil {
				return body.err
			}
			switch code {
			case 0: // AuthenticationOk
			case 3: // cleartext password
				if err := c.sendPasswordMessage(c.cfg.password); err != nil {
					return err
				}
			case 5: // MD5 password
				salt := body.next(4)
				if body.err != nil {
					return body.err
				}
				if err := c.sendPasswordMessage(md5Password(c.cfg.user, c.cfg.password, salt)); err != nil {
					return err
				}
			case 10: // SASL init
				mechanisms := map[string]bool{}
				for {
					name := body.string()
					if name == "" || body.err != nil {
						break
					}
					mechanisms[name] = true
				}
				mechanism := ""
				// Prefer the channel-bound variant: it ties the SCRAM exchange to
				// the TLS session, which defeats a MITM that proxies the handshake.
				if mechanisms["SCRAM-SHA-256-PLUS"] && len(channelBinding) > 0 {
					mechanism = "SCRAM-SHA-256-PLUS"
				} else if mechanisms["SCRAM-SHA-256"] {
					mechanism = "SCRAM-SHA-256"
					channelBinding = nil
				} else {
					return fmt.Errorf("pgwire: server offered no supported SASL mechanism (got %v)", keysOf(mechanisms))
				}
				scram, err = newSCRAMClient(c.cfg.user, c.cfg.password, channelBinding)
				if err != nil {
					return err
				}
				first := scram.firstMessage()
				out := newWriteBuf('p')
				out.string(mechanism)
				out.int32(len(first))
				out.bytes([]byte(first))
				if err := c.writeAndFlush(out); err != nil {
					return err
				}
			case 11: // SASL continue
				if scram == nil {
					return fmt.Errorf("pgwire: SASLContinue before SASLInitialResponse")
				}
				final, err := scram.handleServerFirst(string(body.rest()))
				if err != nil {
					return err
				}
				out := newWriteBuf('p')
				out.bytes([]byte(final))
				if err := c.writeAndFlush(out); err != nil {
					return err
				}
			case 12: // SASL final
				if scram == nil {
					return fmt.Errorf("pgwire: SASLFinal before SASLInitialResponse")
				}
				if err := scram.verifyServerFinal(string(body.rest())); err != nil {
					return err
				}
			case 2:
				return fmt.Errorf("pgwire: Kerberos V5 authentication is not supported")
			case 7:
				return fmt.Errorf("pgwire: GSSAPI authentication is not supported")
			case 9:
				return fmt.Errorf("pgwire: SSPI authentication is not supported")
			default:
				return fmt.Errorf("pgwire: unsupported authentication request %d", code)
			}
		case 'K': // BackendKeyData
			c.backendPID = body.uint32()
			c.secretKey = body.uint32()
			if body.err != nil {
				return body.err
			}
		case 'S': // ParameterStatus
			k := body.string()
			v := body.string()
			if body.err == nil {
				c.params[k] = v
			}
		case 'Z': // ReadyForQuery
			c.txStatus = body.byte()
			// standard_conforming_strings=off would change how backslashes are
			// read in string literals. This driver sends all values as bound
			// parameters, so literals are never built by hand, but a server with
			// it off is unusual enough to be worth refusing rather than guessing.
			if v, ok := c.params["standard_conforming_strings"]; ok && v != "on" {
				return fmt.Errorf("pgwire: server has standard_conforming_strings=%s, refusing to connect", v)
			}
			return nil
		case 'E': // ErrorResponse
			return parseErrorResponse(body)
		case 'N': // NoticeResponse
			// Notices during startup are informational.
		case 'v': // NegotiateProtocolVersion
			return fmt.Errorf("pgwire: server cannot speak protocol 3.0")
		default:
			return fmt.Errorf("pgwire: unexpected message %q during startup", typ)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// md5Password computes the "md5" + hex(md5(md5(password+user)+salt)) response.
//
// MD5 auth is obsolete and Postgres 14+ defaults away from it, but managed
// providers still enable it. It is supported so a deploy does not fail on it,
// and nothing more: prefer scram-sha-256 in pg_hba.conf.
func md5Password(user, password string, salt []byte) string {
	inner := md5.Sum([]byte(password + user))
	outer := md5.New()
	outer.Write([]byte(hex.EncodeToString(inner[:])))
	outer.Write(salt)
	return "md5" + hex.EncodeToString(outer.Sum(nil))
}

func (c *conn) sendPasswordMessage(password string) error {
	w := newWriteBuf('p')
	w.string(password)
	return c.writeAndFlush(w)
}

func (c *conn) writeAndFlush(w *writeBuf) error {
	if _, err := c.w.Write(w.finish()); err != nil {
		c.bad = true
		return fmt.Errorf("pgwire: writing message: %w", err)
	}
	if err := c.w.Flush(); err != nil {
		c.bad = true
		return fmt.Errorf("pgwire: flushing message: %w", err)
	}
	return nil
}

// maxMessageSize caps an incoming message body. Postgres will not legitimately
// send a single message this large to this driver, and without a cap a corrupt
// or hostile length prefix turns into an allocation of up to 2GB.
const maxMessageSize = 256 << 20

// readMessage reads one backend message. The returned readBuf aliases a reusable
// buffer, so its contents are only valid until the next readMessage call.
func (c *conn) readMessage() (byte, *readBuf, error) {
	var header [5]byte
	if _, err := io.ReadFull(c.r, header[:]); err != nil {
		c.bad = true
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil, fmt.Errorf("pgwire: connection closed by server: %w", err)
		}
		return 0, nil, fmt.Errorf("pgwire: reading message header: %w", err)
	}
	typ := header[0]
	length := int(int32(binary.BigEndian.Uint32(header[1:])))
	// Length includes its own 4 bytes.
	if length < 4 {
		c.bad = true
		return 0, nil, fmt.Errorf("pgwire: invalid message length %d for type %q", length, typ)
	}
	bodyLen := length - 4
	if bodyLen > maxMessageSize {
		c.bad = true
		return 0, nil, fmt.Errorf("pgwire: message of %d bytes exceeds the %d byte limit", bodyLen, maxMessageSize)
	}
	if cap(c.scratch) < bodyLen {
		c.scratch = make([]byte, bodyLen)
	}
	body := c.scratch[:bodyLen]
	if _, err := io.ReadFull(c.r, body); err != nil {
		c.bad = true
		return 0, nil, fmt.Errorf("pgwire: reading %d byte body of message %q: %w", bodyLen, typ, err)
	}
	return typ, &readBuf{buf: body}, nil
}

// Close terminates the session.
func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	// Best-effort Terminate so the server frees the backend immediately rather
	// than waiting to notice a dropped socket.
	if !c.bad {
		w := newWriteBuf('X')
		if _, err := c.w.Write(w.finish()); err == nil {
			_ = c.w.Flush()
		}
	}
	return c.netC.Close()
}

// IsValid implements driver.Validator so database/sql drops connections whose
// protocol state is unknown instead of handing them to the next caller.
func (c *conn) IsValid() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && !c.bad && c.txStatus != 'E'
}

// ResetSession implements driver.SessionResetter. A connection returned to the
// pool mid-transaction would otherwise leak that transaction to the next user.
func (c *conn) ResetSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.bad {
		return driver.ErrBadConn
	}
	if c.txStatus != 'I' {
		// Leaving a transaction open would hold locks and pin the xmin horizon.
		if err := c.simpleExec(ctx, "ROLLBACK"); err != nil {
			c.bad = true
			return driver.ErrBadConn
		}
	}
	return nil
}

// cancelRequest asks the server to abort the running query. This travels on a
// brand new connection because the current one is busy streaming results; that
// is how the protocol specifies cancellation.
func (c *conn) cancelRequest() {
	if c.backendPID == 0 {
		return
	}
	netC, err := net.DialTimeout("tcp", c.cfg.addr(), 5*time.Second)
	if err != nil {
		return
	}
	defer netC.Close()
	_ = netC.SetDeadline(time.Now().Add(5 * time.Second))

	// Cancellation must also be sent over TLS when the server requires it.
	if c.cfg.sslMode != sslDisable {
		req := make([]byte, 8)
		binary.BigEndian.PutUint32(req[0:], 8)
		binary.BigEndian.PutUint32(req[4:], 80877103)
		if _, err := netC.Write(req); err == nil {
			resp := make([]byte, 1)
			if _, err := io.ReadFull(netC, resp); err == nil && resp[0] == 'S' {
				tlsConn := tls.Client(netC, &tls.Config{ServerName: c.cfg.host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
				if tlsConn.Handshake() == nil {
					defer tlsConn.Close()
					writeCancel(tlsConn, c.backendPID, c.secretKey)
					return
				}
			}
		}
		return
	}
	writeCancel(netC, c.backendPID, c.secretKey)
}

func writeCancel(w io.Writer, pid, secret uint32) {
	buf := make([]byte, 16)
	binary.BigEndian.PutUint32(buf[0:], 16)
	binary.BigEndian.PutUint32(buf[4:], 80877102) // CancelRequest magic
	binary.BigEndian.PutUint32(buf[8:], pid)
	binary.BigEndian.PutUint32(buf[12:], secret)
	_, _ = w.Write(buf)
}

// nextStatementName returns a session-unique prepared statement name.
func (c *conn) nextStatementName() string {
	c.stmtCounter++
	return "nkstmt_" + strconv.FormatUint(c.stmtCounter, 10)
}

// ServerVersion reports the server_version GUC, for diagnostics.
func (c *conn) ServerVersion() string { return c.params["server_version"] }

var (
	_ driver.Conn               = (*conn)(nil)
	_ driver.Validator          = (*conn)(nil)
	_ driver.SessionResetter    = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
)
