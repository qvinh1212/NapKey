// Package pgtest provides a fake PostgreSQL server for tests.
//
// It speaks enough of the v3 wire protocol for the driver in internal/pgwire to
// connect and run queries, which lets the store layer be exercised against its
// real SQL text and the real scan path without a database process. What it does
// not do is execute SQL: a test declares which rows a query returns. So it
// verifies the Go side of persistence (parameter binding, scanning, error mapping)
// and not the semantics of the SQL itself.
//
// This lives in a normal package rather than a _test.go file so tests in any
// package can import it.
package pgtest

import (
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

// Query is one statement the server received.
type Query struct {
	SQL    string
	Params []string
}

// Column describes a result column.
type Column struct {
	Name string
	// OID is the pg_type OID. Text (25) is the default and works for scanning into
	// most Go types via the driver's text path.
	OID uint32
}

// Response is what a handler returns for a query.
type Response struct {
	Columns []Column
	// Rows holds text-format values. A nil entry is SQL NULL.
	Rows [][]*string
	Tag  string
	// ErrCode, when set, makes the server reply with an ErrorResponse.
	ErrCode       string
	ErrMessage    string
	ErrConstraint string
}

// Handler answers a query.
type Handler func(q Query) Response

// Server is a fake Postgres listener.
type Server struct {
	t        *testing.T
	ln       net.Listener
	password string

	mu       sync.Mutex
	handlers []routedHandler
	fallback Handler
	queries  []Query
	wg       sync.WaitGroup
	closed   bool
}

type routedHandler struct {
	match   string
	handler Handler
}

// New starts a fake server. It trusts every connection, since authentication is
// covered by the driver's own tests.
func New(t *testing.T) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pgtest: listen: %v", err)
	}
	s := &Server{
		t:        t,
		ln:       ln,
		password: "test",
		fallback: func(Query) Response { return Response{Tag: "SELECT 0"} },
	}
	s.wg.Add(1)
	go s.acceptLoop()
	t.Cleanup(s.Close)
	return s
}

// DSN returns a connection string pointing at this server.
func (s *Server) DSN() string {
	addr := s.ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("postgres://napkey:test@127.0.0.1:%d/napkey?sslmode=disable", addr.Port)
}

// On registers a handler for queries containing match. The first registered match
// wins, so more specific patterns should be registered first.
func (s *Server) On(match string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, routedHandler{match: match, handler: h})
}

// OnAny sets the fallback handler.
func (s *Server) OnAny(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fallback = h
}

// Queries returns every statement received so far.
func (s *Server) Queries() []Query {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Query, len(s.queries))
	copy(out, s.queries)
	return out
}

// FindQuery returns the first recorded query containing substr.
func (s *Server) FindQuery(substr string) (Query, bool) {
	for _, q := range s.Queries() {
		if strings.Contains(q.SQL, substr) {
			return q, true
		}
	}
	return Query{}, false
}

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.ln.Close()
	s.wg.Wait()
}

// Text is a helper for a non-NULL text value.
func Text(v string) *string { return &v }

// Null is the NULL value.
var Null *string

func (s *Server) acceptLoop() {
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

func (s *Server) record(q Query) {
	s.mu.Lock()
	s.queries = append(s.queries, q)
	s.mu.Unlock()
}

func (s *Server) dispatch(q Query) Response {
	s.record(q)
	s.mu.Lock()
	handlers := make([]routedHandler, len(s.handlers))
	copy(handlers, s.handlers)
	fallback := s.fallback
	s.mu.Unlock()

	for _, h := range handlers {
		if strings.Contains(q.SQL, h.match) {
			return h.handler(q)
		}
	}
	return fallback(q)
}

// conn tracks per-connection protocol state.
type conn struct {
	net.Conn
	prepared     map[string]string
	boundStmt    string
	boundParams  []string
	resultFormat []int16
	// described caches the response produced at Describe time, keyed by statement
	// name. The extended protocol asks for the row shape before executing, so
	// without this cache a handler would be invoked twice per query and any
	// handler that counts calls or returns different rows over time would see
	// something the caller never asked for.
	described map[string]Response
}

func (s *Server) serve(nc net.Conn) {
	c := &conn{Conn: nc, prepared: map[string]string{}, described: map[string]Response{}}

	startup, err := s.readStartup(c)
	if err != nil {
		return
	}
	if startup == "ssl" {
		// Decline TLS, then read the real startup packet.
		if _, err := c.Write([]byte{'N'}); err != nil {
			return
		}
		if _, err := s.readStartup(c); err != nil {
			return
		}
	}

	// AuthenticationOk, then the parameters the driver checks.
	s.write(c, 'R', func(w *buf) { w.int32(0) })
	s.write(c, 'S', func(w *buf) { w.str("standard_conforming_strings"); w.str("on") })
	s.write(c, 'S', func(w *buf) { w.str("server_version"); w.str("16.2 (pgtest)") })
	s.write(c, 'K', func(w *buf) { w.int32(1234); w.int32(5678) })
	s.ready(c)

	for {
		typ, body, err := readMsg(c)
		if err != nil {
			return
		}
		switch typ {
		case 'Q':
			sql := strings.TrimRight(string(body), "\x00")
			s.respond(c, Query{SQL: sql}, false, true)
		case 'P': // Parse
			r := &reader{b: body}
			name := r.str()
			sql := r.str()
			n := r.int16()
			for i := 0; i < n; i++ {
				r.uint32()
			}
			c.prepared[name] = sql
			s.write(c, '1', nil)
		case 'D': // Describe
			r := &reader{b: body}
			kind := r.byteAt()
			name := r.str()
			if kind != 'S' {
				continue
			}
			sql := c.prepared[name]
			n := countPlaceholders(sql)
			s.write(c, 't', func(w *buf) {
				w.int16(n)
				for i := 0; i < n; i++ {
					w.int32(25) // text
				}
			})
			resp := s.peek(sql)
			c.described[name] = resp
			if len(resp.Columns) == 0 {
				s.write(c, 'n', nil) // NoData
			} else {
				s.rowDescription(c, resp.Columns)
			}
		case 'B': // Bind
			r := &reader{b: body}
			r.str() // portal
			stmtName := r.str()
			nFormats := r.int16()
			for i := 0; i < nFormats; i++ {
				r.int16()
			}
			nParams := r.int16()
			params := make([]string, nParams)
			for i := 0; i < nParams; i++ {
				length := r.int32()
				if length < 0 {
					params[i] = "<NULL>"
					continue
				}
				params[i] = string(r.next(length))
			}
			nResults := r.int16()
			formats := make([]int16, nResults)
			for i := range formats {
				formats[i] = int16(r.int16())
			}
			c.boundStmt = stmtName
			c.boundParams = params
			c.resultFormat = formats
			s.write(c, '2', nil)
		case 'E': // Execute
			sql := c.prepared[c.boundStmt]
			cached, ok := c.described[c.boundStmt]
			s.respondCached(c, Query{SQL: sql, Params: c.boundParams}, cached, ok)
		case 'S': // Sync
			s.ready(c)
		case 'C': // Close
			s.write(c, '3', nil)
		case 'X':
			return
		}
	}
}

// peek asks for a response without recording the query, so Describe does not
// double-count.
func (s *Server) peek(sql string) Response {
	s.mu.Lock()
	handlers := make([]routedHandler, len(s.handlers))
	copy(handlers, s.handlers)
	fallback := s.fallback
	s.mu.Unlock()
	q := Query{SQL: sql}
	for _, h := range handlers {
		if strings.Contains(sql, h.match) {
			return h.handler(q)
		}
	}
	return fallback(q)
}

// respondCached answers an Execute using the response captured at Describe time,
// recording the query without invoking the handler a second time.
func (s *Server) respondCached(c *conn, q Query, cached Response, haveCached bool) {
	if !haveCached {
		s.respond(c, q, true, false)
		return
	}
	s.record(q)
	s.emit(c, cached, true, false)
}

func (s *Server) respond(c *conn, q Query, extended, sendReady bool) {
	resp := s.dispatch(q)
	s.emit(c, resp, extended, sendReady)
}

func (s *Server) emit(c *conn, resp Response, extended, sendReady bool) {
	if resp.ErrCode != "" {
		s.writeErr(c, resp.ErrCode, resp.ErrMessage, resp.ErrConstraint)
		if sendReady {
			s.ready(c)
		}
		return
	}
	if len(resp.Columns) > 0 {
		if !extended {
			s.rowDescription(c, resp.Columns)
		}
		for _, row := range resp.Rows {
			s.write(c, 'D', func(w *buf) {
				w.int16(len(row))
				for i, v := range row {
					if v == nil {
						w.int32(-1)
						continue
					}
					// Tests write values in text. A real server encodes results in
					// whatever format Bind requested, so this has to as well or the
					// driver would be handed data it never asked for.
					out, err := encodeAs(c.formatFor(i), columnOID(resp.Columns, i), *v)
					if err != nil {
						s.t.Errorf("pgtest: cannot encode column %d (%q): %v", i, *v, err)
						out = []byte(*v)
					}
					w.int32(len(out))
					w.raw(out)
				}
			})
		}
	}
	tag := resp.Tag
	if tag == "" {
		tag = fmt.Sprintf("SELECT %d", len(resp.Rows))
	}
	s.write(c, 'C', func(w *buf) { w.str(tag) })
	if sendReady {
		s.ready(c)
	}
}

func (s *Server) rowDescription(c *conn, cols []Column) {
	s.write(c, 'T', func(w *buf) {
		w.int16(len(cols))
		for i, col := range cols {
			w.str(col.Name)
			w.int32(0)
			w.int16(0)
			oid := col.OID
			if oid == 0 {
				oid = 25 // text
			}
			w.int32(int(oid))
			w.int16(-1)
			w.int32(-1)
			w.int16(int(c.formatFor(i)))
		}
	})
}

func (s *Server) readStartup(c *conn) (string, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
		return "", err
	}
	length := int(binary.BigEndian.Uint32(lenBuf[:]))
	if length < 8 || length > 1<<20 {
		return "", fmt.Errorf("pgtest: bad startup length %d", length)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(c, body); err != nil {
		return "", err
	}
	switch binary.BigEndian.Uint32(body) {
	case 80877103:
		return "ssl", nil
	case 80877102:
		return "cancel", nil
	}
	return "startup", nil
}

func (s *Server) write(c *conn, typ byte, fill func(*buf)) {
	w := &buf{}
	w.start(typ)
	if fill != nil {
		fill(w)
	}
	_, _ = c.Write(w.done())
}

func (s *Server) ready(c *conn) {
	s.write(c, 'Z', func(w *buf) { w.raw([]byte{'I'}) })
}

func (s *Server) writeErr(c *conn, code, message, constraint string) {
	s.write(c, 'E', func(w *buf) {
		w.raw([]byte{'S'})
		w.str("ERROR")
		w.raw([]byte{'C'})
		w.str(code)
		w.raw([]byte{'M'})
		w.str(message)
		if constraint != "" {
			w.raw([]byte{'n'})
			w.str(constraint)
		}
		w.raw([]byte{0})
	})
}

// formatFor reports the wire format the client requested for column i. Bind
// carries one code per column, or none at all, in which case text is implied.
func (c *conn) formatFor(i int) int16 {
	if len(c.resultFormat) == 0 {
		return 0
	}
	if len(c.resultFormat) == 1 {
		return c.resultFormat[0]
	}
	if i < len(c.resultFormat) {
		return c.resultFormat[i]
	}
	return 0
}

func columnOID(cols []Column, i int) uint32 {
	if i >= len(cols) || cols[i].OID == 0 {
		return 25 // text
	}
	return cols[i].OID
}

// encodeAs converts a text fixture value into the requested wire format.
func encodeAs(format int16, oid uint32, value string) ([]byte, error) {
	if format == 0 {
		return []byte(value), nil
	}
	switch oid {
	case 16: // bool
		if value == "t" || value == "true" || value == "1" {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case 21: // int2
		n, err := strconv.ParseInt(value, 10, 16)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint16(nil, uint16(int16(n))), nil
	case 23, 26: // int4, oid
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint32(nil, uint32(int32(n))), nil
	case 20: // int8
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint64(nil, uint64(n)), nil
	case 700: // float4
		f, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint32(nil, math.Float32bits(float32(f))), nil
	case 701: // float8
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.AppendUint64(nil, math.Float64bits(f)), nil
	case 1114, 1184: // timestamp, timestamptz
		t, err := parseTimestamp(value)
		if err != nil {
			return nil, err
		}
		micros := t.UTC().Sub(pgEpoch).Microseconds()
		return binary.BigEndian.AppendUint64(nil, uint64(micros)), nil
	case 1082: // date
		t, err := parseTimestamp(value)
		if err != nil {
			return nil, err
		}
		days := int32(t.UTC().Sub(pgEpoch).Hours() / 24)
		return binary.BigEndian.AppendUint32(nil, uint32(days)), nil
	default:
		return []byte(value), nil
	}
}

// pgEpoch is the 2000-01-01 origin Postgres uses for binary timestamps.
var pgEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

var timestampLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999-07",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05-07",
	"2006-01-02 15:04:05",
	"2006-01-02",
	time.RFC3339Nano,
}

func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("pgtest: cannot parse timestamp %q", s)
}

// buf builds one backend message.
type buf struct {
	b      []byte
	lenPos int
}

func (w *buf) start(typ byte) {
	w.b = append(w.b, typ)
	w.lenPos = len(w.b)
	w.b = append(w.b, 0, 0, 0, 0)
}

func (w *buf) int16(n int)  { w.b = binary.BigEndian.AppendUint16(w.b, uint16(int16(n))) }
func (w *buf) int32(n int)  { w.b = binary.BigEndian.AppendUint32(w.b, uint32(int32(n))) }
func (w *buf) raw(p []byte) { w.b = append(w.b, p...) }
func (w *buf) str(s string) { w.b = append(w.b, s...); w.b = append(w.b, 0) }

func (w *buf) done() []byte {
	binary.BigEndian.PutUint32(w.b[w.lenPos:], uint32(len(w.b)-w.lenPos))
	return w.b
}

// reader consumes a frontend message payload.
type reader struct {
	b   []byte
	pos int
}

func (r *reader) byteAt() byte {
	if r.pos >= len(r.b) {
		return 0
	}
	v := r.b[r.pos]
	r.pos++
	return v
}

func (r *reader) int16() int {
	if r.pos+2 > len(r.b) {
		return 0
	}
	v := int16(binary.BigEndian.Uint16(r.b[r.pos:]))
	r.pos += 2
	return int(v)
}

func (r *reader) int32() int {
	if r.pos+4 > len(r.b) {
		return 0
	}
	v := int32(binary.BigEndian.Uint32(r.b[r.pos:]))
	r.pos += 4
	return int(v)
}

func (r *reader) uint32() uint32 {
	if r.pos+4 > len(r.b) {
		return 0
	}
	v := binary.BigEndian.Uint32(r.b[r.pos:])
	r.pos += 4
	return v
}

func (r *reader) str() string {
	for i := r.pos; i < len(r.b); i++ {
		if r.b[i] == 0 {
			s := string(r.b[r.pos:i])
			r.pos = i + 1
			return s
		}
	}
	s := string(r.b[r.pos:])
	r.pos = len(r.b)
	return s
}

func (r *reader) next(n int) []byte {
	if n < 0 || r.pos+n > len(r.b) {
		return nil
	}
	v := r.b[r.pos : r.pos+n]
	r.pos += n
	return v
}

func readMsg(c *conn) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(c, header[:]); err != nil {
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint32(header[1:]))
	if length < 4 || length > 1<<24 {
		return 0, nil, fmt.Errorf("pgtest: bad message length %d", length)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(c, body); err != nil {
		return 0, nil, err
	}
	return header[0], body, nil
}

func countPlaceholders(sql string) int {
	max := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] != '$' {
			continue
		}
		j, n := i+1, 0
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

// UUID returns a deterministic UUID-shaped string for test fixtures.
func UUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// Base64 is a helper for building bytea-ish fixtures.
func Base64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
