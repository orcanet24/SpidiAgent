// database.go - SQLite con manejo concurrente seguro vía connection pool + WAL mode.
// Usa modernc.org/sqlite (driver puro Go, no requiere cgo).
package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DB envuelve sql.DB con cola de escrituras y control de concurrencia.
type DB struct {
	pool    *sql.DB
	writeCh chan writeOp
	wg      sync.WaitGroup
	mu      sync.RWMutex
	logger  *slog.Logger
	closed  chan struct{}
}

type writeOp struct {
	fn   func(*sql.DB) error
	done chan error
}

// Config para abrir la base de datos.
type Config struct {
	Path     string
	Log      *slog.Logger
	PoolSize int // conexiones al pool (default 10)
}

// Open abre una SQLite con WAL mode y pool de conexiones.
func Open(cfg Config) (*DB, error) {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 10
	}

	// Crear directorio si no existe
	dir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("database: mkdir: %w", err)
	}

	// connection string con pragmas para WAL + performance
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=ON&_cache_size=-64000", cfg.Path)
	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("database: open: %w", err)
	}

	pool.SetMaxOpenConns(cfg.PoolSize)
	pool.SetMaxIdleConns(cfg.PoolSize / 2)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(10 * time.Minute)

	db := &DB{
		pool:    pool,
		writeCh: make(chan writeOp, 1000), // buffer para picos de escritura
		logger:  cfg.Log,
		closed:  make(chan struct{}),
	}

	// Iniciar worker serializado de escrituras
	db.wg.Add(1)
	go db.writeWorker()

	if err := db.migrate(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: migrate: %w", err)
	}

	cfg.Log.Info("database opened", "path", cfg.Path, "wal", true)
	return db, nil
}

// writeWorker procesa escrituras serializadas para evitar SQLITE_BUSY.
func (db *DB) writeWorker() {
	defer db.wg.Done()
	for {
		select {
		case op := <-db.writeCh:
			err := op.fn(db.pool)
			if op.done != nil {
				op.done <- err
			}
		case <-db.closed:
			// vaciar cola pendiente
			for {
				select {
				case op := <-db.writeCh:
					err := op.fn(db.pool)
					if op.done != nil {
						op.done <- err
					}
				default:
					return
				}
			}
		}
	}
}

// Write envía una operación de escritura al worker serializado.
func (db *DB) Write(fn func(*sql.DB) error) error {
	done := make(chan error, 1)
	select {
	case db.writeCh <- writeOp{fn: fn, done: done}:
		return <-done
	case <-db.closed:
		return fmt.Errorf("database: closed")
	}
}

// QueryRow ejecuta una consulta de lectura directa (pool).
func (db *DB) QueryRow(query string, args ...interface{}) *sql.Row {
	return db.pool.QueryRow(query, args...)
}

// Query ejecuta múltiples filas de lectura directa (pool).
func (db *DB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.pool.Query(query, args...)
}

// Exec ejecuta una escritura directa (sin cola).
func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.pool.Exec(query, args...)
}

// Close cierra la base de datos limpiamente.
func (db *DB) Close() error {
	close(db.closed)
	db.wg.Wait()
	return db.pool.Close()
}

// Underlying retiene el pool para transacciones complejas.
func (db *DB) Underlying() *sql.DB {
	return db.pool
}

// migrate crea las tablas si no existen.
func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		short_name TEXT UNIQUE NOT NULL,
		password_encrypted TEXT NOT NULL,
		agreement_id TEXT,
		environment TEXT NOT NULL DEFAULT 'sandbox',
		callback_base_url TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_tenants_short_name ON tenants(short_name);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		order_id TEXT NOT NULL,
		payment_url TEXT NOT NULL,
		currency TEXT NOT NULL,
		amount REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		spidi_session_id TEXT,
		expires_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (tenant_id) REFERENCES tenants(id)
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_tenant ON sessions(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_order ON sessions(order_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);

	CREATE TABLE IF NOT EXISTS operation_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id TEXT NOT NULL,
		operation TEXT NOT NULL,
		resource_id TEXT,
		status TEXT NOT NULL,
		request_json TEXT,
		response_json TEXT,
		error_message TEXT,
		duration_ms INTEGER,
		client_ip TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (tenant_id) REFERENCES tenants(id)
	);
	CREATE INDEX IF NOT EXISTS idx_log_tenant ON operation_log(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_log_created ON operation_log(created_at);
	CREATE INDEX IF NOT EXISTS idx_log_operation ON operation_log(operation);

	CREATE TABLE IF NOT EXISTS audit_chain (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		table_name TEXT NOT NULL,
		row_id TEXT NOT NULL,
		action TEXT NOT NULL,
		before_json TEXT,
		after_json TEXT,
		actor TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_audit_row ON audit_chain(table_name, row_id);
	`
	_, err := db.pool.Exec(schema)
	return err
}
