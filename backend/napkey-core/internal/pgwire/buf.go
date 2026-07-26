package pgwire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// writeBuf builds a single frontend message. Every message is
// [type byte][int32 length including itself][payload], so the length is
// backfilled once the payload is known.
type writeBuf struct {
	buf []byte
	// lenPos is where the 4-byte length placeholder sits.
	lenPos int
}

func newWriteBuf(msgType byte) *writeBuf {
	w := &writeBuf{buf: make([]byte, 0, 64)}
	w.start(msgType)
	return w
}

func (w *writeBuf) start(msgType byte) {
	if msgType != 0 {
		w.buf = append(w.buf, msgType)
	}
	w.lenPos = len(w.buf)
	w.buf = append(w.buf, 0, 0, 0, 0)
}

func (w *writeBuf) byte(b byte) { w.buf = append(w.buf, b) }

func (w *writeBuf) int16(n int) {
	w.buf = binary.BigEndian.AppendUint16(w.buf, uint16(n))
}

func (w *writeBuf) int32(n int) {
	w.buf = binary.BigEndian.AppendUint32(w.buf, uint32(n))
}

// string writes a C string: the bytes followed by a NUL terminator. Postgres
// identifiers and SQL text are NUL-terminated on the wire, so an embedded NUL
// would truncate the value; callers must reject those before getting here.
func (w *writeBuf) string(s string) {
	w.buf = append(w.buf, s...)
	w.buf = append(w.buf, 0)
}

func (w *writeBuf) bytes(b []byte) { w.buf = append(w.buf, b...) }

// finish backfills the length prefix and returns the complete message.
func (w *writeBuf) finish() []byte {
	binary.BigEndian.PutUint32(w.buf[w.lenPos:], uint32(len(w.buf)-w.lenPos))
	return w.buf
}

// readBuf consumes a backend message payload. Every accessor checks remaining
// length: a truncated or hostile payload must produce an error, never a panic
// inside a database call.
type readBuf struct {
	buf []byte
	pos int
	err error
}

var errShortMessage = errors.New("pgwire: backend message truncated")

func (r *readBuf) remaining() int { return len(r.buf) - r.pos }

func (r *readBuf) fail(e error) {
	if r.err == nil {
		r.err = e
	}
}

func (r *readBuf) byte() byte {
	if r.remaining() < 1 {
		r.fail(errShortMessage)
		return 0
	}
	b := r.buf[r.pos]
	r.pos++
	return b
}

func (r *readBuf) int16() int {
	if r.remaining() < 2 {
		r.fail(errShortMessage)
		return 0
	}
	v := int16(binary.BigEndian.Uint16(r.buf[r.pos:]))
	r.pos += 2
	return int(v)
}

func (r *readBuf) int32() int {
	if r.remaining() < 4 {
		r.fail(errShortMessage)
		return 0
	}
	v := int32(binary.BigEndian.Uint32(r.buf[r.pos:]))
	r.pos += 4
	return int(v)
}

func (r *readBuf) uint32() uint32 {
	if r.remaining() < 4 {
		r.fail(errShortMessage)
		return 0
	}
	v := binary.BigEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v
}

// string reads a NUL-terminated string.
func (r *readBuf) string() string {
	for i := r.pos; i < len(r.buf); i++ {
		if r.buf[i] == 0 {
			s := string(r.buf[r.pos:i])
			r.pos = i + 1
			return s
		}
	}
	r.fail(errShortMessage)
	return ""
}

// next returns n bytes as a subslice of the read buffer. The result aliases the
// connection's scratch space, so anything retained past the current message
// must be copied.
func (r *readBuf) next(n int) []byte {
	if n < 0 || r.remaining() < n {
		r.fail(errShortMessage)
		return nil
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b
}

// rest returns everything not yet consumed.
func (r *readBuf) rest() []byte {
	b := r.buf[r.pos:]
	r.pos = len(r.buf)
	return b
}

// checkInt16Count guards the field counts that appear in RowDescription and
// DataRow. A negative count would otherwise become a huge make() below.
func checkInt16Count(n int, what string) (int, error) {
	if n < 0 {
		return 0, fmt.Errorf("pgwire: negative %s count %d", what, n)
	}
	if n > math.MaxInt16 {
		return 0, fmt.Errorf("pgwire: %s count %d exceeds protocol maximum", what, n)
	}
	return n, nil
}
