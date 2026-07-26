package pgwire

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// sslMode mirrors libpq's sslmode values.
type sslMode string

const (
	sslDisable    sslMode = "disable"
	sslPrefer     sslMode = "prefer"
	sslRequire    sslMode = "require"
	sslVerifyCA   sslMode = "verify-ca"
	sslVerifyFull sslMode = "verify-full"
)

// dsnConfig is a parsed connection string.
type dsnConfig struct {
	host     string
	port     string
	user     string
	password string
	database string
	sslMode  sslMode
	// sslRootCert is a PEM bundle path used by verify-ca and verify-full.
	sslRootCert string
	// appName shows up in pg_stat_activity, which makes it obvious which service
	// owns a slow query.
	appName string
	// connectTimeout bounds dial + handshake.
	connectTimeout time.Duration
	// runtimeParams are forwarded in the startup packet (search_path, timezone...).
	runtimeParams map[string]string
}

func (c *dsnConfig) addr() string { return net.JoinHostPort(c.host, c.port) }

// parseDSN accepts either a URL form (postgres://user:pass@host:port/db?k=v) or
// the space-separated key=value form (host=... user=...).
func parseDSN(dsn string) (*dsnConfig, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("pgwire: empty connection string")
	}
	c := &dsnConfig{
		host:           "localhost",
		port:           "5432",
		sslMode:        sslPrefer,
		appName:        "napkey-core",
		connectTimeout: 10 * time.Second,
		runtimeParams:  map[string]string{},
	}

	var kv map[string]string
	var err error
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		kv, err = parseURLDSN(dsn)
	} else {
		kv, err = parseKeywordDSN(dsn)
	}
	if err != nil {
		return nil, err
	}

	for k, v := range kv {
		switch k {
		case "host":
			c.host = v
		case "port":
			c.port = v
		case "user":
			c.user = v
		case "password":
			c.password = v
		case "dbname", "database":
			c.database = v
		case "sslmode":
			c.sslMode = sslMode(strings.ToLower(v))
		case "sslrootcert":
			c.sslRootCert = v
		case "application_name":
			c.appName = v
		case "connect_timeout":
			// libpq measures connect_timeout in seconds.
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return nil, fmt.Errorf("pgwire: connect_timeout %q is not an integer", v)
			}
			if n > 0 {
				c.connectTimeout = time.Duration(n) * time.Second
			}
		case "search_path", "timezone", "TimeZone", "statement_timeout",
			"idle_in_transaction_session_timeout", "lock_timeout", "options":
			c.runtimeParams[k] = v
		default:
			// Unknown keys become runtime parameters, matching libpq. A typo
			// then fails loudly at startup instead of being ignored.
			c.runtimeParams[k] = v
		}
	}

	if c.user == "" {
		return nil, fmt.Errorf("pgwire: user is required in the connection string")
	}
	if c.database == "" {
		// libpq defaults dbname to the user name.
		c.database = c.user
	}
	switch c.sslMode {
	case sslDisable, sslPrefer, sslRequire, sslVerifyCA, sslVerifyFull:
	default:
		return nil, fmt.Errorf("pgwire: unsupported sslmode %q", c.sslMode)
	}
	if _, err := strconv.Atoi(c.port); err != nil {
		return nil, fmt.Errorf("pgwire: port %q is not numeric", c.port)
	}
	// A NUL byte would terminate the value early in the startup packet and could
	// smuggle in a different database name.
	for k, v := range map[string]string{"user": c.user, "dbname": c.database, "application_name": c.appName} {
		if strings.ContainsRune(v, 0) {
			return nil, fmt.Errorf("pgwire: %s contains a NUL byte", k)
		}
	}
	return c, nil
}

func parseURLDSN(dsn string) (map[string]string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgwire: invalid connection URL: %w", err)
	}
	kv := map[string]string{}
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			kv["user"] = name
		}
		if pw, ok := u.User.Password(); ok {
			kv["password"] = pw
		}
	}
	if h := u.Hostname(); h != "" {
		kv["host"] = h
	}
	if p := u.Port(); p != "" {
		kv["port"] = p
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		kv["dbname"] = db
	}
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			kv[k] = vs[len(vs)-1]
		}
	}
	return kv, nil
}

// parseKeywordDSN handles "host=x user=y password='a b'" including single-quoted
// values and backslash escapes, per libpq rules.
func parseKeywordDSN(dsn string) (map[string]string, error) {
	kv := map[string]string{}
	runes := []rune(dsn)
	i := 0
	for i < len(runes) {
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		if i >= len(runes) {
			break
		}
		keyStart := i
		for i < len(runes) && runes[i] != '=' && runes[i] != ' ' && runes[i] != '\t' {
			i++
		}
		key := string(runes[keyStart:i])
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		if i >= len(runes) || runes[i] != '=' {
			return nil, fmt.Errorf("pgwire: missing '=' after %q in connection string", key)
		}
		i++
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		var val strings.Builder
		if i < len(runes) && runes[i] == '\'' {
			i++
			for i < len(runes) && runes[i] != '\'' {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++
				}
				val.WriteRune(runes[i])
				i++
			}
			if i >= len(runes) {
				return nil, fmt.Errorf("pgwire: unterminated quoted value for %q", key)
			}
			i++
		} else {
			for i < len(runes) && runes[i] != ' ' && runes[i] != '\t' {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++
				}
				val.WriteRune(runes[i])
				i++
			}
		}
		if key == "" {
			return nil, fmt.Errorf("pgwire: empty key in connection string")
		}
		kv[key] = val.String()
	}
	return kv, nil
}
