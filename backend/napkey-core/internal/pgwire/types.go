package pgwire

import (
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Type OIDs from pg_type. Only the types this project stores are listed.
const (
	oidBool        uint32 = 16
	oidBytea       uint32 = 17
	oidChar        uint32 = 18
	oidName        uint32 = 19
	oidInt8        uint32 = 20
	oidInt2        uint32 = 21
	oidInt4        uint32 = 23
	oidText        uint32 = 25
	oidOID         uint32 = 26
	oidJSON        uint32 = 114
	oidXML         uint32 = 142
	oidFloat4      uint32 = 700
	oidFloat8      uint32 = 701
	oidUnknown     uint32 = 705
	oidBPChar      uint32 = 1042
	oidVarchar     uint32 = 1043
	oidDate        uint32 = 1082
	oidTime        uint32 = 1083
	oidTimestamp   uint32 = 1114
	oidTimestamptz uint32 = 1184
	oidInterval    uint32 = 1186
	oidNumeric     uint32 = 1700
	oidUUID        uint32 = 2950
	oidJSONB       uint32 = 3802
	oidTextArray   uint32 = 1009
	oidVarcharArr  uint32 = 1015
	oidInt4Array   uint32 = 1007
	oidInt8Array   uint32 = 1016
	oidInet        uint32 = 869
)

// formatText and formatBinary are the two wire formats.
const (
	formatText   int16 = 0
	formatBinary int16 = 1
)

// pgEpoch is 2000-01-01 UTC, the origin Postgres uses for binary timestamps.
var pgEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// resultFormat picks the wire format to request for a result column.
//
// Binary is used only where it is unambiguous and cheaper: fixed-width numbers,
// booleans, bytea, and timestamps. Text is kept for numeric (arbitrary
// precision, whose binary form is a base-10000 digit array) and for anything
// unrecognized, where the text form is what the server can always produce.
func resultFormat(oid uint32) int16 {
	switch oid {
	case oidBool, oidInt2, oidInt4, oidInt8, oidFloat4, oidFloat8,
		oidBytea, oidTimestamp, oidTimestamptz, oidDate, oidOID:
		return formatBinary
	default:
		return formatText
	}
}

// paramFormat picks the wire format for a bound parameter.
func paramFormat(v driver.Value, oid uint32) int16 {
	if v == nil {
		return formatText
	}
	switch oid {
	case oidBool, oidInt2, oidInt4, oidInt8, oidFloat4, oidFloat8, oidBytea:
		return formatBinary
	case oidTimestamp, oidTimestamptz:
		if _, ok := v.(time.Time); ok {
			return formatBinary
		}
		return formatText
	default:
		return formatText
	}
}

// encodeParam converts a driver.Value into wire bytes. A nil return means SQL NULL.
func encodeParam(v driver.Value, oid uint32, format int16) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	if format == formatBinary {
		return encodeBinaryParam(v, oid)
	}
	return encodeTextParam(v, oid)
}

func encodeBinaryParam(v driver.Value, oid uint32) ([]byte, error) {
	switch val := v.(type) {
	case bool:
		if val {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case int64:
		switch oid {
		case oidInt2:
			if val < math.MinInt16 || val > math.MaxInt16 {
				return nil, fmt.Errorf("value %d overflows smallint", val)
			}
			return binary.BigEndian.AppendUint16(nil, uint16(int16(val))), nil
		case oidInt4:
			if val < math.MinInt32 || val > math.MaxInt32 {
				return nil, fmt.Errorf("value %d overflows integer", val)
			}
			return binary.BigEndian.AppendUint32(nil, uint32(int32(val))), nil
		default:
			return binary.BigEndian.AppendUint64(nil, uint64(val)), nil
		}
	case float64:
		if oid == oidFloat4 {
			return binary.BigEndian.AppendUint32(nil, math.Float32bits(float32(val))), nil
		}
		return binary.BigEndian.AppendUint64(nil, math.Float64bits(val)), nil
	case []byte:
		return val, nil
	case time.Time:
		// Binary timestamps are microseconds since 2000-01-01.
		micros := val.UTC().Sub(pgEpoch).Microseconds()
		return binary.BigEndian.AppendUint64(nil, uint64(micros)), nil
	case string:
		return []byte(val), nil
	default:
		return encodeTextParam(v, oid)
	}
}

func encodeTextParam(v driver.Value, oid uint32) ([]byte, error) {
	switch val := v.(type) {
	case string:
		if strings.ContainsRune(val, 0) {
			// Postgres text cannot hold a NUL. Rejecting is better than the
			// silent truncation a C-string write would produce.
			return nil, fmt.Errorf("string parameter contains a NUL byte")
		}
		return []byte(val), nil
	case bool:
		if val {
			return []byte("t"), nil
		}
		return []byte("f"), nil
	case int64:
		return []byte(strconv.FormatInt(val, 10)), nil
	case float64:
		if math.IsNaN(val) {
			return []byte("NaN"), nil
		}
		if math.IsInf(val, 1) {
			return []byte("Infinity"), nil
		}
		if math.IsInf(val, -1) {
			return []byte("-Infinity"), nil
		}
		return []byte(strconv.FormatFloat(val, 'g', -1, 64)), nil
	case []byte:
		if oid == oidBytea {
			return []byte(`\x` + hex.EncodeToString(val)), nil
		}
		return val, nil
	case time.Time:
		// RFC3339 with microseconds; the server parses this for every date/time type.
		return []byte(val.UTC().Format("2006-01-02 15:04:05.999999-07:00")), nil
	default:
		return nil, fmt.Errorf("unsupported parameter type %T", v)
	}
}

// decodeValue turns wire bytes into a driver.Value.
//
// database/sql only accepts a fixed set of kinds from a driver, so everything
// lands on one of: bool, int64, float64, []byte, string, or time.Time. Callers
// scanning into a *string or a custom Scanner get the conversion done above this
// layer by database/sql itself.
func decodeValue(data []byte, oid uint32, format int16) (driver.Value, error) {
	if format == formatBinary {
		return decodeBinary(data, oid)
	}
	return decodeText(data, oid)
}

func decodeBinary(data []byte, oid uint32) (driver.Value, error) {
	switch oid {
	case oidBool:
		if len(data) != 1 {
			return nil, fmt.Errorf("bool payload is %d bytes, want 1", len(data))
		}
		return data[0] != 0, nil
	case oidInt2:
		if len(data) != 2 {
			return nil, fmt.Errorf("smallint payload is %d bytes, want 2", len(data))
		}
		return int64(int16(binary.BigEndian.Uint16(data))), nil
	case oidInt4, oidOID:
		if len(data) != 4 {
			return nil, fmt.Errorf("integer payload is %d bytes, want 4", len(data))
		}
		return int64(int32(binary.BigEndian.Uint32(data))), nil
	case oidInt8:
		if len(data) != 8 {
			return nil, fmt.Errorf("bigint payload is %d bytes, want 8", len(data))
		}
		return int64(binary.BigEndian.Uint64(data)), nil
	case oidFloat4:
		if len(data) != 4 {
			return nil, fmt.Errorf("real payload is %d bytes, want 4", len(data))
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(data))), nil
	case oidFloat8:
		if len(data) != 8 {
			return nil, fmt.Errorf("double payload is %d bytes, want 8", len(data))
		}
		return math.Float64frombits(binary.BigEndian.Uint64(data)), nil
	case oidBytea:
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	case oidTimestamp, oidTimestamptz:
		if len(data) != 8 {
			return nil, fmt.Errorf("timestamp payload is %d bytes, want 8", len(data))
		}
		micros := int64(binary.BigEndian.Uint64(data))
		// Postgres encodes -infinity/infinity as the int64 extremes.
		if micros == math.MinInt64 || micros == math.MaxInt64 {
			return nil, fmt.Errorf("timestamp is infinite, which has no time.Time representation")
		}
		return pgEpoch.Add(time.Duration(micros) * time.Microsecond), nil
	case oidDate:
		if len(data) != 4 {
			return nil, fmt.Errorf("date payload is %d bytes, want 4", len(data))
		}
		days := int32(binary.BigEndian.Uint32(data))
		return pgEpoch.AddDate(0, 0, int(days)), nil
	default:
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	}
}

func decodeText(data []byte, oid uint32) (driver.Value, error) {
	s := string(data)
	switch oid {
	case oidBool:
		return s == "t" || s == "true" || s == "1", nil
	case oidInt2, oidInt4, oidInt8, oidOID:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing integer %q: %w", s, err)
		}
		return n, nil
	case oidFloat4, oidFloat8:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing float %q: %w", s, err)
		}
		return f, nil
	case oidBytea:
		if strings.HasPrefix(s, `\x`) {
			out, err := hex.DecodeString(s[2:])
			if err != nil {
				return nil, fmt.Errorf("parsing bytea hex: %w", err)
			}
			return out, nil
		}
		return []byte(s), nil
	case oidTimestamp, oidTimestamptz, oidDate:
		t, err := parsePgTimestamp(s)
		if err != nil {
			return nil, err
		}
		return t, nil
	case oidNumeric:
		// numeric stays a string so the caller decides how to interpret it.
		// Money in this project is bigint micro-VND (DESIGN.md section 5), so
		// numeric only shows up in reporting aggregates, never in a balance.
		return []byte(s), nil
	default:
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	}
}

// pgTimestampLayouts covers the DateStyle=ISO output shapes, with and without
// fractional seconds and timezone offset.
var pgTimestampLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999-07",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05-07",
	"2006-01-02 15:04:05",
	"2006-01-02",
	time.RFC3339Nano,
}

func parsePgTimestamp(s string) (time.Time, error) {
	if s == "infinity" || s == "-infinity" {
		return time.Time{}, fmt.Errorf("timestamp %q has no time.Time representation", s)
	}
	for _, layout := range pgTimestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q", s)
}

// typeName maps an OID to its SQL name for ColumnTypeDatabaseTypeName.
func typeName(oid uint32) string {
	switch oid {
	case oidBool:
		return "BOOL"
	case oidBytea:
		return "BYTEA"
	case oidInt2:
		return "INT2"
	case oidInt4:
		return "INT4"
	case oidInt8:
		return "INT8"
	case oidText:
		return "TEXT"
	case oidVarchar, oidBPChar:
		return "VARCHAR"
	case oidFloat4:
		return "FLOAT4"
	case oidFloat8:
		return "FLOAT8"
	case oidNumeric:
		return "NUMERIC"
	case oidUUID:
		return "UUID"
	case oidJSON:
		return "JSON"
	case oidJSONB:
		return "JSONB"
	case oidDate:
		return "DATE"
	case oidTime:
		return "TIME"
	case oidTimestamp:
		return "TIMESTAMP"
	case oidTimestamptz:
		return "TIMESTAMPTZ"
	case oidInet:
		return "INET"
	case oidTextArray, oidVarcharArr:
		return "TEXT[]"
	default:
		return "OID" + strconv.FormatUint(uint64(oid), 10)
	}
}
