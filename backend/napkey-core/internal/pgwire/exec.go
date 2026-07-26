package pgwire

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Ping implements driver.Pinger.
func (c *conn) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.bad {
		return driver.ErrBadConn
	}
	return c.simpleExec(ctx, ";")
}

// simpleExec runs a statement through the simple query protocol and discards any
// result rows. Used for transaction control and health checks.
func (c *conn) simpleExec(ctx context.Context, sql string) error {
	w := newWriteBuf('Q')
	w.string(sql)
	if err := c.writeAndFlush(w); err != nil {
		return err
	}
	return c.drainUntilReady(ctx, nil)
}

// drainUntilReady consumes messages through ReadyForQuery, collecting the first
// error. It always runs to ReadyForQuery even after an error, because leaving
// unread messages buffered would desynchronize every later query on this
// connection.
func (c *conn) drainUntilReady(ctx context.Context, onRow func(*readBuf) error) error {
	var firstErr error
	canceled := false
	for {
		// Cancellation is checked between messages: the server has to be told to
		// stop, and the reply still has to be drained.
		if !canceled && ctx.Err() != nil {
			canceled = true
			c.cancelRequest()
		}
		typ, body, err := c.readMessage()
		if err != nil {
			c.bad = true
			if firstErr == nil {
				firstErr = err
			}
			return firstErr
		}
		switch typ {
		case 'Z':
			c.txStatus = body.byte()
			if firstErr != nil {
				return firstErr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		case 'E':
			pgErr := parseErrorResponse(body)
			// A cancellation surfaces as 57014; report the context error so
			// callers can tell a timeout from a real query failure.
			if pgErr.Code == CodeQueryCanceled && ctx.Err() != nil {
				if firstErr == nil {
					firstErr = ctx.Err()
				}
			} else if firstErr == nil {
				firstErr = pgErr
			}
		case 'D':
			if onRow != nil {
				if err := onRow(body); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		case 'C', 'T', 'I', 'S', 'N', 'n', '1', '2', '3', 't', 'K', 'A':
			// CommandComplete, RowDescription, EmptyQueryResponse, ParameterStatus,
			// Notice, NoData, Parse/Bind/Close complete, ParameterDescription,
			// BackendKeyData, NotificationResponse: nothing to do while draining.
			if typ == 'S' {
				k := body.string()
				v := body.string()
				if body.err == nil {
					c.params[k] = v
				}
			}
		case 'G', 'H', 'W':
			if firstErr == nil {
				firstErr = errors.New("pgwire: COPY is not supported by this driver")
			}
			c.bad = true
		default:
			if firstErr == nil {
				firstErr = fmt.Errorf("pgwire: unexpected message %q", typ)
			}
		}
	}
}

// stmt is a prepared statement.
type stmt struct {
	c        *conn
	name     string
	sql      string
	numInput int
	// paramOIDs is what the server inferred for each placeholder; used to encode
	// arguments in the form the server expects.
	paramOIDs []uint32
	fields    []fieldDesc
	closed    bool
}

// fieldDesc describes one result column.
type fieldDesc struct {
	name    string
	typeOID uint32
	format  int16
	typeMod int32
}

// Prepare implements driver.Conn.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// PrepareContext implements driver.ConnPrepareContext.
func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.bad {
		return nil, driver.ErrBadConn
	}
	if strings.ContainsRune(query, 0) {
		return nil, errors.New("pgwire: query contains a NUL byte")
	}
	name := c.nextStatementName()

	parse := newWriteBuf('P')
	parse.string(name)
	parse.string(query)
	parse.int16(0) // let the server infer parameter types
	if _, err := c.w.Write(parse.finish()); err != nil {
		c.bad = true
		return nil, fmt.Errorf("pgwire: writing Parse: %w", err)
	}
	describe := newWriteBuf('D')
	describe.byte('S')
	describe.string(name)
	if _, err := c.w.Write(describe.finish()); err != nil {
		c.bad = true
		return nil, fmt.Errorf("pgwire: writing Describe: %w", err)
	}
	sync := newWriteBuf('S')
	if err := c.writeAndFlush(sync); err != nil {
		return nil, err
	}

	s := &stmt{c: c, name: name, sql: query}
	var firstErr error
	for {
		typ, body, err := c.readMessage()
		if err != nil {
			c.bad = true
			return nil, err
		}
		switch typ {
		case '1': // ParseComplete
		case 't': // ParameterDescription
			n, cErr := checkInt16Count(body.int16(), "parameter")
			if cErr != nil {
				c.bad = true
				return nil, cErr
			}
			s.paramOIDs = make([]uint32, n)
			for i := range s.paramOIDs {
				s.paramOIDs[i] = body.uint32()
			}
			s.numInput = n
		case 'T': // RowDescription
			fields, fErr := parseRowDescription(body)
			if fErr != nil {
				c.bad = true
				return nil, fErr
			}
			s.fields = fields
		case 'n': // NoData (statement returns no rows)
		case 'E':
			if firstErr == nil {
				firstErr = parseErrorResponse(body)
			}
		case 'S':
			k := body.string()
			v := body.string()
			if body.err == nil {
				c.params[k] = v
			}
		case 'N':
		case 'Z':
			c.txStatus = body.byte()
			if firstErr != nil {
				return nil, firstErr
			}
			if body.err != nil {
				return nil, body.err
			}
			return s, nil
		default:
			if firstErr == nil {
				firstErr = fmt.Errorf("pgwire: unexpected message %q while preparing", typ)
			}
		}
	}
}

func parseRowDescription(body *readBuf) ([]fieldDesc, error) {
	n, err := checkInt16Count(body.int16(), "field")
	if err != nil {
		return nil, err
	}
	fields := make([]fieldDesc, n)
	for i := range fields {
		fields[i].name = body.string()
		body.uint32() // table OID
		body.int16()  // column attribute number
		fields[i].typeOID = body.uint32()
		body.int16() // type size
		fields[i].typeMod = int32(body.int32())
		fields[i].format = int16(body.int16())
	}
	if body.err != nil {
		return nil, body.err
	}
	return fields, nil
}

// Close releases the server-side statement.
func (s *stmt) Close() error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.c.closed || s.c.bad {
		return nil
	}
	w := newWriteBuf('C')
	w.byte('S')
	w.string(s.name)
	if _, err := s.c.w.Write(w.finish()); err != nil {
		s.c.bad = true
		return nil
	}
	sync := newWriteBuf('S')
	if err := s.c.writeAndFlush(sync); err != nil {
		return nil
	}
	return s.c.drainUntilReady(context.Background(), nil)
}

// NumInput implements driver.Stmt.
func (s *stmt) NumInput() int { return s.numInput }

// Exec implements driver.Stmt for the legacy path.
func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), valuesToNamed(args))
}

// Query implements driver.Stmt for the legacy path.
func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), valuesToNamed(args))
}

func valuesToNamed(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

// ExecContext implements driver.StmtExecContext.
func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.closed || s.c.bad {
		return nil, driver.ErrBadConn
	}
	if _, err := s.c.bindExecute(ctx, s, args, false); err != nil {
		return nil, err
	}
	res := &result{}
	err := s.c.collectResult(ctx, res, nil)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// QueryContext implements driver.StmtQueryContext.
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.closed || s.c.bad {
		return nil, driver.ErrBadConn
	}
	resultFormats, err := s.c.bindExecute(ctx, s, args, true)
	if err != nil {
		return nil, err
	}
	// Rows are buffered rather than streamed. Streaming would hold the
	// connection mutex across the caller's iteration, and every query in this
	// service is bounded by LIMIT, so buffering trades a little memory for a much
	// simpler lifetime. A query returning an unbounded set would need revisiting.
	rows := &rowsResult{fields: s.fields, formats: resultFormats}
	res := &result{}
	if err := s.c.collectResult(ctx, res, rows); err != nil {
		return nil, err
	}
	if rows.fields == nil {
		rows.fields = res.lastFields
	}
	return rows, nil
}

// bindExecute writes Bind + Execute + Sync for a prepared statement.
func (c *conn) bindExecute(ctx context.Context, s *stmt, args []driver.NamedValue, wantRows bool) ([]int16, error) {
	if len(args) != s.numInput {
		return nil, fmt.Errorf("pgwire: statement expects %d parameters, got %d", s.numInput, len(args))
	}
	bind := newWriteBuf('B')
	bind.string("") // unnamed portal
	bind.string(s.name)

	// One format code per parameter, chosen from the OID the server inferred.
	bind.int16(len(args))
	formats := make([]int16, len(args))
	for i, a := range args {
		var oid uint32
		if i < len(s.paramOIDs) {
			oid = s.paramOIDs[i]
		}
		formats[i] = paramFormat(a.Value, oid)
		bind.int16(int(formats[i]))
	}

	bind.int16(len(args))
	for i, a := range args {
		var oid uint32
		if i < len(s.paramOIDs) {
			oid = s.paramOIDs[i]
		}
		encoded, err := encodeParam(a.Value, oid, formats[i])
		if err != nil {
			return nil, fmt.Errorf("pgwire: encoding parameter $%d: %w", i+1, err)
		}
		if encoded == nil {
			bind.int32(-1) // NULL
			continue
		}
		bind.int32(len(encoded))
		bind.bytes(encoded)
	}

	// Request binary results for the columns that decode unambiguously that way.
	var resultFormats []int16
	if wantRows && len(s.fields) > 0 {
		bind.int16(len(s.fields))
		resultFormats = make([]int16, len(s.fields))
		for i, f := range s.fields {
			resultFormats[i] = resultFormat(f.typeOID)
			bind.int16(int(resultFormats[i]))
		}
	} else {
		bind.int16(0)
	}

	if _, err := c.w.Write(bind.finish()); err != nil {
		c.bad = true
		return nil, fmt.Errorf("pgwire: writing Bind: %w", err)
	}

	exec := newWriteBuf('E')
	exec.string("") // unnamed portal
	exec.int32(0)   // no row limit
	if _, err := c.w.Write(exec.finish()); err != nil {
		c.bad = true
		return nil, fmt.Errorf("pgwire: writing Execute: %w", err)
	}
	sync := newWriteBuf('S')
	if err := c.writeAndFlush(sync); err != nil {
		return nil, err
	}
	return resultFormats, nil
}

// collectResult reads through ReadyForQuery, filling res and optionally rows.
func (c *conn) collectResult(ctx context.Context, res *result, rows *rowsResult) error {
	onRow := func(body *readBuf) error {
		if rows == nil {
			return nil
		}
		return rows.appendRow(body)
	}
	var firstErr error
	canceled := false
	for {
		if !canceled && ctx.Err() != nil {
			canceled = true
			c.cancelRequest()
		}
		typ, body, err := c.readMessage()
		if err != nil {
			c.bad = true
			return err
		}
		switch typ {
		case 'D':
			if e := onRow(body); e != nil && firstErr == nil {
				firstErr = e
			}
		case 'T':
			fields, fErr := parseRowDescription(body)
			if fErr != nil {
				c.bad = true
				return fErr
			}
			res.lastFields = fields
			if rows != nil && rows.fields == nil {
				rows.fields = fields
			}
		case 'C':
			tag := body.string()
			res.applyTag(tag)
		case 'I': // EmptyQueryResponse
		case '2', '1', '3', 'n', 't':
		case 'S':
			k := body.string()
			v := body.string()
			if body.err == nil {
				c.params[k] = v
			}
		case 'N':
		case 'A':
		case 'E':
			pgErr := parseErrorResponse(body)
			if pgErr.Code == CodeQueryCanceled && ctx.Err() != nil {
				if firstErr == nil {
					firstErr = ctx.Err()
				}
			} else if firstErr == nil {
				firstErr = pgErr
			}
		case 'Z':
			c.txStatus = body.byte()
			if firstErr != nil {
				return firstErr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		case 's': // PortalSuspended, only with a row limit
		case 'G', 'H', 'W':
			c.bad = true
			return errors.New("pgwire: COPY is not supported by this driver")
		default:
			if firstErr == nil {
				firstErr = fmt.Errorf("pgwire: unexpected message %q in result stream", typ)
			}
		}
	}
}

// ExecContext implements driver.ExecerContext. Statements with no parameters go
// through the simple protocol, which saves a Parse/Describe round trip; anything
// with parameters is prepared so values are never interpolated into SQL.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) > 0 {
		return nil, driver.ErrSkip // let database/sql prepare it
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.bad {
		return nil, driver.ErrBadConn
	}
	if strings.ContainsRune(query, 0) {
		return nil, errors.New("pgwire: query contains a NUL byte")
	}
	w := newWriteBuf('Q')
	w.string(query)
	if err := c.writeAndFlush(w); err != nil {
		return nil, err
	}
	res := &result{}
	if err := c.collectResult(ctx, res, nil); err != nil {
		return nil, err
	}
	return res, nil
}

// QueryContext implements driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) > 0 {
		return nil, driver.ErrSkip
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.bad {
		return nil, driver.ErrBadConn
	}
	if strings.ContainsRune(query, 0) {
		return nil, errors.New("pgwire: query contains a NUL byte")
	}
	w := newWriteBuf('Q')
	w.string(query)
	if err := c.writeAndFlush(w); err != nil {
		return nil, err
	}
	rows := &rowsResult{}
	res := &result{}
	if err := c.collectResult(ctx, res, rows); err != nil {
		return nil, err
	}
	if rows.fields == nil {
		rows.fields = res.lastFields
	}
	return rows, nil
}

// result carries RowsAffected from CommandComplete.
type result struct {
	rowsAffected int64
	lastInsertID int64
	haveRows     bool
	lastFields   []fieldDesc
}

func (r *result) applyTag(tag string) {
	// Tags look like "INSERT 0 12", "UPDATE 3", "SELECT 7".
	parts := strings.Fields(tag)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "INSERT":
		if len(parts) >= 3 {
			if oid, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				r.lastInsertID = oid
			}
			if n, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
				r.rowsAffected += n
				r.haveRows = true
			}
		}
	case "UPDATE", "DELETE", "SELECT", "MOVE", "FETCH", "COPY", "MERGE":
		if len(parts) >= 2 {
			if n, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				r.rowsAffected += n
				r.haveRows = true
			}
		}
	}
}

func (r *result) LastInsertId() (int64, error) {
	// Postgres does not have a general last-insert-id. The OID in the INSERT tag
	// is zero unless the table has OIDs, which is not the case for anything here.
	// Returning an error is more honest than returning 0; use RETURNING instead.
	return 0, errors.New("pgwire: LastInsertId is not supported, use INSERT ... RETURNING id")
}

func (r *result) RowsAffected() (int64, error) {
	if !r.haveRows {
		return 0, nil
	}
	return r.rowsAffected, nil
}

// rowsResult is a fully buffered result set.
type rowsResult struct {
	fields []fieldDesc
	// formats is the wire format per column. It is recorded from what the query
	// path actually asked for rather than recomputed at decode time: the simple
	// query protocol always answers in text, while the extended protocol answers
	// in whatever Bind requested. Deriving it from the type OID would decode
	// simple-protocol bigints as if they were binary.
	formats []int16
	// data holds one slice per row; nil entries are SQL NULL.
	data [][][]byte
	pos  int
}

// formatFor returns the wire format of column i, defaulting to text.
func (rs *rowsResult) formatFor(i int) int16 {
	if i < len(rs.formats) {
		return rs.formats[i]
	}
	return formatText
}

func (rs *rowsResult) appendRow(body *readBuf) error {
	n, err := checkInt16Count(body.int16(), "column")
	if err != nil {
		return err
	}
	row := make([][]byte, n)
	for i := 0; i < n; i++ {
		length := body.int32()
		if body.err != nil {
			return body.err
		}
		if length < 0 {
			row[i] = nil // NULL
			continue
		}
		src := body.next(length)
		if body.err != nil {
			return body.err
		}
		// The read buffer is reused between messages, so the bytes must be copied.
		buf := make([]byte, length)
		copy(buf, src)
		row[i] = buf
	}
	rs.data = append(rs.data, row)
	return nil
}

// Columns implements driver.Rows.
func (rs *rowsResult) Columns() []string {
	out := make([]string, len(rs.fields))
	for i, f := range rs.fields {
		out[i] = f.name
	}
	return out
}

// Close implements driver.Rows.
func (rs *rowsResult) Close() error {
	rs.data = nil
	rs.pos = 0
	return nil
}

// Next implements driver.Rows.
func (rs *rowsResult) Next(dest []driver.Value) error {
	if rs.pos >= len(rs.data) {
		return io.EOF
	}
	row := rs.data[rs.pos]
	rs.pos++
	for i := range dest {
		if i >= len(row) {
			dest[i] = nil
			continue
		}
		if row[i] == nil {
			dest[i] = nil
			continue
		}
		var oid uint32
		if i < len(rs.fields) {
			oid = rs.fields[i].typeOID
		}
		v, err := decodeValue(row[i], oid, rs.formatFor(i))
		if err != nil {
			return fmt.Errorf("pgwire: decoding column %d (%s): %w", i, rs.columnName(i), err)
		}
		dest[i] = v
	}
	return nil
}

func (rs *rowsResult) columnName(i int) string {
	if i < len(rs.fields) {
		return rs.fields[i].name
	}
	return "?"
}

// ColumnTypeDatabaseTypeName implements driver.RowsColumnTypeDatabaseTypeName so
// sql.ColumnType reports a useful name.
func (rs *rowsResult) ColumnTypeDatabaseTypeName(index int) string {
	if index >= len(rs.fields) {
		return ""
	}
	return typeName(rs.fields[index].typeOID)
}

var (
	_ driver.Stmt             = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
	_ driver.StmtQueryContext = (*stmt)(nil)
	_ driver.Rows             = (*rowsResult)(nil)
	_ driver.Result           = (*result)(nil)
)
