// Package pgwire is a database/sql driver for PostgreSQL speaking the v3
// frontend/backend protocol directly.
//
// # Why this exists
//
// napkey-core is built in an environment with no access to the Go module proxy,
// so github.com/jackc/pgx could not be vendored. Everything above this package
// is written against database/sql and never imports pgwire outside of a single
// blank import in main, so replacing this with pgx is an import swap plus a DSN
// scheme change, not a rewrite.
//
// # What it implements
//
//	startup + SCRAM-SHA-256, MD5, and cleartext password auth
//	the extended query protocol (Parse/Bind/Describe/Execute/Sync) for queries
//	  carrying parameters, and the simple protocol for those that do not
//	explicit transactions with all four isolation levels and read-only mode
//	TLS (sslmode=disable|prefer|require|verify-ca|verify-full)
//	binary and text result decoding for the types this project stores
//	context cancellation via out-of-band CancelRequest on a second connection
//
// # What it does not implement
//
//	COPY, LISTEN/NOTIFY, cursors, prepared statement caching across
//	connections, and array types beyond text[]. The queries in this service do
//	not use them; a query that does will return an explicit error rather than
//	silently misbehaving.
//
// The driver registers itself as "postgres" and accepts postgres:// and
// postgresql:// URLs as well as space-separated key=value DSNs.
package pgwire
