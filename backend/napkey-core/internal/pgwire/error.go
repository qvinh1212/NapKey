package pgwire

import (
	"errors"
	"fmt"
	"strings"
)

// Error is a Postgres ErrorResponse. Code is the five-character SQLSTATE, which
// is the only part of the message worth branching on: the human text is
// localized and can change between versions.
type Error struct {
	Severity   string
	Code       string
	Message    string
	Detail     string
	Hint       string
	Position   string
	Where      string
	Schema     string
	Table      string
	Column     string
	Constraint string
	File       string
	Line       string
	Routine    string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("pgwire: ")
	if e.Severity != "" {
		b.WriteString(e.Severity)
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Code != "" {
		fmt.Fprintf(&b, " (SQLSTATE %s)", e.Code)
	}
	if e.Detail != "" {
		b.WriteString(", detail: ")
		b.WriteString(e.Detail)
	}
	if e.Constraint != "" {
		b.WriteString(", constraint: ")
		b.WriteString(e.Constraint)
	}
	return b.String()
}

// SQLSTATE codes this project acts on.
const (
	// CodeUniqueViolation is how a duplicate email or a replayed webhook event
	// surfaces. Callers turn it into a domain-specific conflict rather than a 500.
	CodeUniqueViolation     = "23505"
	CodeForeignKeyViolation = "23503"
	CodeCheckViolation      = "23514"
	// CodeExclusionViolation is how an EXCLUDE constraint rejects a row, which in
	// this schema means two price periods for one model would overlap.
	CodeExclusionViolation   = "23P01"
	CodeNotNullViolation     = "23502"
	CodeSerializationFailure = "40001"
	CodeDeadlockDetected     = "40P01"
	CodeInvalidPassword      = "28P01"
	CodeInvalidAuthorization = "28000"
	CodeUndefinedTable       = "42P01"
	CodeQueryCanceled        = "57014"
	CodeAdminShutdown        = "57P01"
	CodeCrashShutdown        = "57P02"
	CodeCannotConnectNow     = "57P03"
)

// IsUniqueViolation reports whether err is a unique-constraint violation,
// optionally narrowed to a named constraint. Passing the constraint name matters
// when a table has more than one unique index and the caller only wants to
// swallow one of them.
func IsUniqueViolation(err error, constraint ...string) bool {
	var pgErr *Error
	if !errors.As(err, &pgErr) || pgErr.Code != CodeUniqueViolation {
		return false
	}
	if len(constraint) == 0 {
		return true
	}
	for _, c := range constraint {
		if pgErr.Constraint == c {
			return true
		}
	}
	return false
}

// IsExclusionViolation reports whether err is an exclusion-constraint violation.
// The price book uses one to guarantee a model never has two prices covering the
// same instant.
func IsExclusionViolation(err error) bool {
	var pgErr *Error
	return errors.As(err, &pgErr) && pgErr.Code == CodeExclusionViolation
}

// IsSerializationFailure reports whether the transaction should be retried.
func IsSerializationFailure(err error) bool {
	var pgErr *Error
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == CodeSerializationFailure || pgErr.Code == CodeDeadlockDetected
}

// SQLState returns the SQLSTATE of err, or "" when err is not a Postgres error.
func SQLState(err error) string {
	var pgErr *Error
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// parseErrorResponse decodes the field-tagged ErrorResponse/NoticeResponse body.
func parseErrorResponse(r *readBuf) *Error {
	e := &Error{}
	for {
		typ := r.byte()
		if typ == 0 || r.err != nil {
			break
		}
		val := r.string()
		switch typ {
		case 'S':
			e.Severity = val
		case 'C':
			e.Code = val
		case 'M':
			e.Message = val
		case 'D':
			e.Detail = val
		case 'H':
			e.Hint = val
		case 'P':
			e.Position = val
		case 'W':
			e.Where = val
		case 's':
			e.Schema = val
		case 't':
			e.Table = val
		case 'c':
			e.Column = val
		case 'n':
			e.Constraint = val
		case 'F':
			e.File = val
		case 'L':
			e.Line = val
		case 'R':
			e.Routine = val
		}
	}
	if e.Message == "" {
		e.Message = "unknown server error"
	}
	return e
}
