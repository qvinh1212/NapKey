package pgwire

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer speaks enough of the PostgreSQL v3 protocol to exercise the driver
// end to end: startup, SCRAM-SHA-256, the simple query protocol, and the
// extended query protocol.
//
// This exists because the driver is the one piece of napkey-core that cannot be
// verified by reasoning about types alone; a wire protocol is either byte-correct
// or it is broken. Testing against a server that computes real SCRAM proofs
// catches the mistakes that matter (wrong auth message composition, bad length
// prefixes, mishandled NULs) which a mock returning canned bytes would not.
type fakeServer struct {
	t        *testing.T
	ln       net.Listener
	password string
	// handler answers a query with columns and rows, or an error.
	handler func(q fakeQuery) fakeResponse

	mu      sync.Mutex
	queries []fakeQuery
	wg      sync.WaitGroup
	closing bool
	// authMethod selects which authentication exchange to run.
	authMethod string
}

type fakeQuery struct {
	SQL      string
	Params   [][]byte
	Extended bool
}

type fakeResponse struct {
	// Fields describes result columns. Empty means a command with no result set.
	Fields []fakeField
	Rows   [][][]byte
	Tag    string
	// ErrCode, when set, makes the server reply with ErrorResponse instead.
	ErrCode       string
	ErrMessage    string
	ErrConstraint string
}

type fakeField struct {
	Name   string
	OID    uint32
	Format int16
}

func newFakeServer(t *testing.T, password string) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeServer{
		t:          t,
		ln:         ln,
		password:   password,
		authMethod: "scram",
		handler: func(fakeQuery) fakeResponse {
			return fakeResponse{Tag: "SELECT 0"}
		},
	}
	s.wg.Add(1)
	go s.acceptLoop()
	t.Cleanup(s.Close)
	return s
}

func (s *fakeServer) dsn() string {
	addr := s.ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("postgres://napkey:%s@127.0.0.1:%d/napkey?sslmode=disable", s.password, addr.Port)
}

func (s *fakeServer) Close() {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.mu.Unlock()
	s.ln.Close()
	s.wg.Wait()
}

func (s *fakeServer) recordedQueries() []fakeQuery {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]fakeQuery, len(s.queries))
	copy(out, s.queries)
	return out
}

func (s *fakeServer) setHandler(h func(fakeQuery) fakeResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

func (s *fakeServer) acceptLoop() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer c.Close()
			s.serve(c)
		}()
	}
}

// serverConn wraps a connection with the read/write helpers the server side needs.
type serverConn struct {
	net.Conn
	// preparedSQL maps statement name to SQL text.
	preparedSQL map[string]string
	// boundParams holds the parameters of the current portal.
	boundParams [][]byte
	boundStmt   string
	// boundResultFormats is the per-column result format the client asked for in
	// Bind. A real server encodes its reply accordingly, so the fake has to as
	// well or the driver would be tested against data it never requested.
	boundResultFormats []int16
}

func (s *fakeServer) serve(nc net.Conn) {
	c := &serverConn{Conn: nc, preparedSQL: map[string]string{}}

	// Startup: length-prefixed with no type byte.
	startup, err := readStartupPacket(c)
	if err != nil {
		return
	}
	if code, ok := startup["__sslrequest__"]; ok && code == "1" {
		// Refuse TLS, then read the real startup packet.
		if _, err := c.Write([]byte{'N'}); err != nil {
			return
		}
		startup, err = readStartupPacket(c)
		if err != nil {
			return
		}
	}

	switch s.authMethod {
	case "scram":
		if err := s.doSCRAM(c); err != nil {
			s.writeError(c, "28P01", err.Error(), "")
			return
		}
	case "cleartext":
		s.writeMsg(c, 'R', func(w *writeBuf) { w.int32(3) })
		typ, body, err := readClientMsg(c)
		if err != nil || typ != 'p' {
			return
		}
		got := strings.TrimRight(string(body), "\x00")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.password)) != 1 {
			s.writeError(c, "28P01", "password authentication failed", "")
			return
		}
	case "trust":
		// no challenge
	}

	// AuthenticationOk
	s.writeMsg(c, 'R', func(w *writeBuf) { w.int32(0) })
	// The driver refuses to connect unless this is "on".
	s.writeParam(c, "standard_conforming_strings", "on")
	s.writeParam(c, "server_version", "16.2 (fake)")
	s.writeParam(c, "client_encoding", "UTF8")
	s.writeMsg(c, 'K', func(w *writeBuf) {
		w.int32(4242)
		w.int32(99999)
	})
	s.writeReady(c, 'I')

	for {
		typ, body, err := readClientMsg(c)
		if err != nil {
			return
		}
		switch typ {
		case 'Q':
			sql := strings.TrimRight(string(body), "\x00")
			s.handleQuery(c, fakeQuery{SQL: sql}, true)
		case 'P': // Parse
			r := &readBuf{buf: body}
			name := r.string()
			sql := r.string()
			nParams := r.int16()
			for i := 0; i < nParams; i++ {
				r.uint32()
			}
			c.preparedSQL[name] = sql
			s.writeMsg(c, '1', nil) // ParseComplete
		case 'D': // Describe
			r := &readBuf{buf: body}
			kind := r.byte()
			name := r.string()
			if kind == 'S' {
				sql := c.preparedSQL[name]
				n := countPlaceholders(sql)
				s.writeMsg(c, 't', func(w *writeBuf) {
					w.int16(n)
					for i := 0; i < n; i++ {
						w.int32(int(oidText))
					}
				})
				resp := s.dispatch(fakeQuery{SQL: sql, Extended: true})
				if len(resp.Fields) == 0 {
					s.writeMsg(c, 'n', nil) // NoData
				} else {
					// Format is not known until Bind, so Describe reports 0.
					described := make([]fakeField, len(resp.Fields))
					copy(described, resp.Fields)
					for i := range described {
						described[i].Format = formatText
					}
					s.writeRowDescription(c, described)
				}
			}
		case 'B': // Bind
			r := &readBuf{buf: body}
			r.string() // portal
			stmtName := r.string()
			nFormats := r.int16()
			formats := make([]int16, nFormats)
			for i := range formats {
				formats[i] = int16(r.int16())
			}
			nParams := r.int16()
			params := make([][]byte, nParams)
			for i := 0; i < nParams; i++ {
				length := r.int32()
				if length < 0 {
					params[i] = nil
					continue
				}
				b := r.next(length)
				cp := make([]byte, len(b))
				copy(cp, b)
				params[i] = cp
			}
			nResultFormats := r.int16()
			resultFormats := make([]int16, nResultFormats)
			for i := range resultFormats {
				resultFormats[i] = int16(r.int16())
			}
			c.boundStmt = stmtName
			c.boundParams = params
			c.boundResultFormats = resultFormats
			s.writeMsg(c, '2', nil) // BindComplete
		case 'E': // Execute
			sql := c.preparedSQL[c.boundStmt]
			s.handleQuery(c, fakeQuery{SQL: sql, Params: c.boundParams, Extended: true}, false)
			c.boundResultFormats = nil
		case 'S': // Sync
			s.writeReady(c, 'I')
		case 'C': // Close
			s.writeMsg(c, '3', nil) // CloseComplete
		case 'X': // Terminate
			return
		default:
			s.writeError(c, "08P01", fmt.Sprintf("fake server got unexpected message %q", typ), "")
			s.writeReady(c, 'I')
		}
	}
}

func (s *fakeServer) dispatch(q fakeQuery) fakeResponse {
	s.mu.Lock()
	h := s.handler
	s.mu.Unlock()
	return h(q)
}

func (s *fakeServer) handleQuery(c *serverConn, q fakeQuery, sendReady bool) {
	s.mu.Lock()
	s.queries = append(s.queries, q)
	h := s.handler
	s.mu.Unlock()

	resp := h(q)
	if resp.ErrCode != "" {
		s.writeError(c, resp.ErrCode, resp.ErrMessage, resp.ErrConstraint)
		if sendReady {
			s.writeReady(c, 'I')
		}
		return
	}
	if len(resp.Fields) > 0 {
		// In the extended protocol RowDescription already went out with Describe.
		if !q.Extended {
			s.writeRowDescription(c, resp.Fields)
		}
		for _, row := range resp.Rows {
			s.writeMsg(c, 'D', func(w *writeBuf) {
				w.int16(len(row))
				for i, col := range row {
					if col == nil {
						w.int32(-1)
						continue
					}
					out, err := s.encodeColumn(c, resp.Fields, i, col)
					if err != nil {
						s.t.Errorf("fake server cannot encode column %d: %v", i, err)
						out = col
					}
					w.int32(len(out))
					w.bytes(out)
				}
			})
		}
	}
	tag := resp.Tag
	if tag == "" {
		tag = "SELECT " + fmt.Sprint(len(resp.Rows))
	}
	s.writeMsg(c, 'C', func(w *writeBuf) { w.string(tag) })
	if sendReady {
		s.writeReady(c, 'I')
	}
}

// encodeColumn converts test-supplied data into the format the client requested.
//
// fakeField.Format declares the format the test wrote its bytes in (text by
// default); the client's Bind decides what goes on the wire. Without this the
// harness would answer a binary request with text and the driver would look
// broken when it was not.
func (s *fakeServer) encodeColumn(c *serverConn, fields []fakeField, i int, data []byte) ([]byte, error) {
	declared := formatText
	var oid uint32
	if i < len(fields) {
		declared = fields[i].Format
		oid = fields[i].OID
	}
	want := formatText
	if i < len(c.boundResultFormats) {
		want = c.boundResultFormats[i]
	}
	if declared == want {
		return data, nil
	}
	if want == formatBinary {
		return textToBinary(oid, string(data))
	}
	return binaryToText(oid, data)
}

func textToBinary(oid uint32, s string) ([]byte, error) {
	switch oid {
	case oidBool:
		if s == "t" || s == "true" || s == "1" {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case oidInt2:
		n, err := strconv.ParseInt(s, 10, 16)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint16(nil, uint16(int16(n))), nil
	case oidInt4, oidOID:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint32(nil, uint32(int32(n))), nil
	case oidInt8:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint64(nil, uint64(n)), nil
	case oidFloat4:
		f, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint32(nil, math.Float32bits(float32(f))), nil
	case oidFloat8:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint64(nil, math.Float64bits(f)), nil
	case oidTimestamp, oidTimestamptz:
		t, err := parsePgTimestamp(s)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint64(nil, uint64(t.UTC().Sub(pgEpoch).Microseconds())), nil
	case oidDate:
		t, err := parsePgTimestamp(s)
		if err != nil {
			return nil, err
		}
		days := int32(t.UTC().Sub(pgEpoch).Hours() / 24)
		return binary.BigEndian.AppendUint32(nil, uint32(days)), nil
	case oidBytea:
		return []byte(s), nil
	default:
		return []byte(s), nil
	}
}

func binaryToText(oid uint32, data []byte) ([]byte, error) {
	v, err := decodeBinary(data, oid)
	if err != nil {
		return nil, err
	}
	switch val := v.(type) {
	case bool:
		if val {
			return []byte("t"), nil
		}
		return []byte("f"), nil
	case int64:
		return []byte(strconv.FormatInt(val, 10)), nil
	case float64:
		return []byte(strconv.FormatFloat(val, 'g', -1, 64)), nil
	case time.Time:
		return []byte(val.UTC().Format("2006-01-02 15:04:05.999999-07:00")), nil
	case []byte:
		return val, nil
	default:
		return nil, fmt.Errorf("cannot render %T as text", v)
	}
}

func (s *fakeServer) writeRowDescription(c *serverConn, fields []fakeField) {
	s.writeMsg(c, 'T', func(w *writeBuf) {
		w.int16(len(fields))
		for _, f := range fields {
			w.string(f.Name)
			w.int32(0)          // table OID
			w.int16(0)          // column number
			w.int32(int(f.OID)) // type OID
			w.int16(-1)         // type size
			w.int32(-1)         // type modifier
			w.int16(int(f.Format))
		}
	})
}

func (s *fakeServer) doSCRAM(c *serverConn) error {
	s.writeMsg(c, 'R', func(w *writeBuf) {
		w.int32(10)
		w.string("SCRAM-SHA-256")
		w.byte(0)
	})

	typ, body, err := readClientMsg(c)
	if err != nil {
		return fmt.Errorf("reading SASLInitialResponse: %w", err)
	}
	if typ != 'p' {
		return fmt.Errorf("expected SASLInitialResponse, got %q", typ)
	}
	r := &readBuf{buf: body}
	mechanism := r.string()
	if mechanism != "SCRAM-SHA-256" {
		return fmt.Errorf("unexpected mechanism %q", mechanism)
	}
	length := r.int32()
	clientFirst := string(r.next(length))

	// Strip the GS2 header to get client-first-message-bare.
	parts := strings.SplitN(clientFirst, ",", 3)
	if len(parts) < 3 {
		return fmt.Errorf("malformed client-first-message %q", clientFirst)
	}
	clientFirstBare := parts[2]
	attrs, err := parseSCRAMAttrs(clientFirstBare)
	if err != nil {
		return err
	}
	clientNonce := attrs["r"]
	if clientNonce == "" {
		return fmt.Errorf("client sent no nonce")
	}

	serverNoncePart := make([]byte, 16)
	if _, err := rand.Read(serverNoncePart); err != nil {
		return err
	}
	serverNonce := clientNonce + base64.StdEncoding.EncodeToString(serverNoncePart)
	salt := []byte("napkey-fake-salt")
	const iterations = 4096
	serverFirst := fmt.Sprintf("r=%s,s=%s,i=%d", serverNonce,
		base64.StdEncoding.EncodeToString(salt), iterations)

	s.writeMsg(c, 'R', func(w *writeBuf) {
		w.int32(11)
		w.bytes([]byte(serverFirst))
	})

	typ, body, err = readClientMsg(c)
	if err != nil {
		return fmt.Errorf("reading SASLResponse: %w", err)
	}
	if typ != 'p' {
		return fmt.Errorf("expected SASLResponse, got %q", typ)
	}
	clientFinal := string(body)
	proofIdx := strings.LastIndex(clientFinal, ",p=")
	if proofIdx < 0 {
		return fmt.Errorf("client-final-message has no proof")
	}
	clientFinalWithoutProof := clientFinal[:proofIdx]
	proofB64 := clientFinal[proofIdx+3:]
	gotProof, err := base64.StdEncoding.DecodeString(proofB64)
	if err != nil {
		return fmt.Errorf("proof is not base64: %w", err)
	}

	authMessage := clientFirstBare + "," + serverFirst + "," + clientFinalWithoutProof
	saltedPassword, err := pbkdf2.Key(sha256.New, s.password, salt, iterations, sha256.Size)
	if err != nil {
		return err
	}
	clientKey := hmacSHA256(saltedPassword, []byte("Client Key"))
	storedKey := sha256.Sum256(clientKey)
	clientSignature := hmacSHA256(storedKey[:], []byte(authMessage))
	wantProof := make([]byte, len(clientKey))
	for i := range clientKey {
		wantProof[i] = clientKey[i] ^ clientSignature[i]
	}
	if subtle.ConstantTimeCompare(gotProof, wantProof) != 1 {
		return fmt.Errorf("password authentication failed for user \"napkey\"")
	}

	serverKey := hmacSHA256(saltedPassword, []byte("Server Key"))
	serverSignature := hmacSHA256(serverKey, []byte(authMessage))
	s.writeMsg(c, 'R', func(w *writeBuf) {
		w.int32(12)
		w.bytes([]byte("v=" + base64.StdEncoding.EncodeToString(serverSignature)))
	})
	return nil
}

func (s *fakeServer) writeMsg(c *serverConn, typ byte, fill func(*writeBuf)) {
	w := newWriteBuf(typ)
	if fill != nil {
		fill(w)
	}
	if _, err := c.Write(w.finish()); err != nil {
		return
	}
}

func (s *fakeServer) writeParam(c *serverConn, k, v string) {
	s.writeMsg(c, 'S', func(w *writeBuf) {
		w.string(k)
		w.string(v)
	})
}

func (s *fakeServer) writeReady(c *serverConn, status byte) {
	s.writeMsg(c, 'Z', func(w *writeBuf) { w.byte(status) })
}

func (s *fakeServer) writeError(c *serverConn, code, message, constraint string) {
	s.writeMsg(c, 'E', func(w *writeBuf) {
		w.byte('S')
		w.string("ERROR")
		w.byte('C')
		w.string(code)
		w.byte('M')
		w.string(message)
		if constraint != "" {
			w.byte('n')
			w.string(constraint)
		}
		w.byte(0)
	})
}

// readStartupPacket reads the untyped startup or SSLRequest packet.
func readStartupPacket(c *serverConn) (map[string]string, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint32(lenBuf[:]))
	if length < 8 || length > 1<<20 {
		return nil, fmt.Errorf("bad startup length %d", length)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(c, body); err != nil {
		return nil, err
	}
	r := &readBuf{buf: body}
	version := r.uint32()
	if version == 80877103 {
		return map[string]string{"__sslrequest__": "1"}, nil
	}
	if version == 80877102 {
		return map[string]string{"__cancel__": "1"}, nil
	}
	out := map[string]string{}
	for {
		k := r.string()
		if k == "" || r.err != nil {
			break
		}
		out[k] = r.string()
	}
	return out, nil
}

// readClientMsg reads one typed frontend message.
func readClientMsg(c *serverConn) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(c, header[:]); err != nil {
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint32(header[1:]))
	if length < 4 || length > 1<<24 {
		return 0, nil, fmt.Errorf("bad message length %d", length)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(c, body); err != nil {
		return 0, nil, err
	}
	return header[0], body, nil
}

// countPlaceholders finds the highest $N in the SQL so the fake server can report
// a matching parameter count.
func countPlaceholders(sql string) int {
	max := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] != '$' {
			continue
		}
		j := i + 1
		n := 0
		for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
			n = n*10 + int(sql[j]-'0')
			j++
		}
		if n > max {
			max = n
		}
		i = j - 1
	}
	return max
}
