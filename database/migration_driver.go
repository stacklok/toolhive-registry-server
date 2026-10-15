package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	migratedb "github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"

	"github.com/stacklok/toolhive-registry-server/internal/db/exposuretarget"
)

var errMigrationBackendUnconfirmed = errors.New(
	"migration backend termination unconfirmed; remain offline and inspect the runner session and backend")

// migrationDriver implements the pinned migrate Driver contract on one owned
// connection. Unlike postgres.WithConnection, every query uses the runner context.
// The mutex joins migrate's asynchronous Lock call before Close releases resources.
type migrationDriver struct {
	mu           sync.Mutex
	ctx          context.Context
	conn         *pgx.Conn
	control      *pgx.Conn
	lockID       string
	locked       bool
	closed       bool
	closeErr     error
	pid          uint32
	backendStart time.Time
	backendProbe func(context.Context, *pgx.Conn, uint32, time.Time) (bool, error)
}

func newMigrationDriver(ctx context.Context, connString string, control *pgx.Conn) (*migrationDriver, error) {
	if strings.TrimSpace(connString) == "" {
		return nil, errors.New("invalid migration connection configuration")
	}
	config, err := pgx.ParseConfig(connString)
	if err != nil {
		return nil, errors.New("invalid migration connection configuration")
	}
	config.BuildContextWatcherHandler = func(conn *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: conn, DeadlineDelay: 5 * time.Second}
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("migration connection unavailable")
	}
	d := &migrationDriver{ctx: ctx, conn: conn, control: control, pid: conn.PgConn().PID(), backendProbe: migrationBackendExists}
	err = conn.QueryRow(ctx, `SELECT backend_start FROM pg_stat_activity WHERE pid=pg_backend_pid()`).Scan(&d.backendStart)
	if err == nil && control != nil {
		err = lockSchemaMigration(ctx, conn)
	}
	var name string
	if err == nil {
		err = conn.QueryRow(ctx, `SELECT current_database()`).Scan(&name)
	}
	if err == nil {
		d.lockID, err = migratedb.GenerateAdvisoryLockId(name, "public", "schema_migrations")
	}
	if err == nil {
		err = d.Lock()
	}
	if err == nil {
		var exists bool
		err = conn.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&exists)
		if err == nil && !exists {
			_, err = conn.Exec(ctx, `CREATE TABLE public.schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)`)
		}
		err = errors.Join(err, d.Unlock())
	}
	if err != nil {
		return nil, errors.Join(d.failure(err), d.Close())
	}
	return d, nil
}

func (*migrationDriver) Open(string) (migratedb.Driver, error) {
	return nil, errors.New("migration driver requires an owned connection")
}

func (d *migrationDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		d.closed = true
		d.closeErr = d.close()
	}
	return d.closeErr
}

func (d *migrationDriver) Lock() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.locked {
		return migratedb.ErrLocked
	}
	// Finish before migrate's independent 15s lock timeout can abandon this call.
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()
	_, err := d.conn.Exec(ctx, `SELECT pg_advisory_lock($1::text::bigint)`, d.lockID)
	if err == nil {
		d.locked = true
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return d.failure(err)
}

func (d *migrationDriver) Unlock() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.locked {
		return migratedb.ErrNotLocked
	}
	if d.conn.IsClosed() {
		return errors.New("migration connection closed; lock release requires backend exit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Failed migration scripts may leave an explicit transaction aborted.
	_, err := d.conn.Exec(ctx, `ROLLBACK; SELECT pg_advisory_unlock(`+fmt.Sprint(d.lockID)+`)`)
	if err == nil {
		d.locked = false
	}
	return d.failure(err)
}

func (d *migrationDriver) Run(r io.Reader) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if strings.Contains(string(data), "-- End eligibility:") {
		return d.runCleanup(string(data))
	}
	_, err = d.conn.Exec(d.ctx, string(data))
	return d.failure(err)
}

func (d *migrationDriver) SetVersion(version int, dirty bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.conn.Begin(d.ctx)
	if err != nil {
		return d.failure(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(ctx)
	}()
	if _, err = tx.Exec(d.ctx, `TRUNCATE public.schema_migrations`); err == nil && (version >= 0 || dirty) {
		_, err = tx.Exec(d.ctx, `INSERT INTO public.schema_migrations(version,dirty) VALUES($1,$2)`, version, dirty)
	}
	if err == nil {
		err = tx.Commit(d.ctx)
	}
	return d.failure(err)
}

func (d *migrationDriver) Version() (int, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var version int
	var dirty bool
	err := d.conn.QueryRow(d.ctx, `SELECT version,dirty FROM public.schema_migrations LIMIT 1`).Scan(&version, &dirty)
	if errors.Is(err, pgx.ErrNoRows) {
		return migratedb.NilVersion, false, nil
	}
	return version, dirty, d.failure(err)
}

func (*migrationDriver) Drop() error {
	return errors.New("dropping the migration database is unsupported")
}

func (d *migrationDriver) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := d.conn.Close(ctx)
	select {
	case <-d.conn.PgConn().CleanupDone():
	case <-ctx.Done():
		return errMigrationBackendUnconfirmed
	}
	if d.backendStart.IsZero() {
		return errMigrationBackendUnconfirmed
	}
	control := d.control
	if control == nil {
		// Raw migrators have no caller session available for observation.
		var e error
		control, e = pgx.Connect(ctx, d.conn.Config().ConnString())
		if e != nil {
			return errMigrationBackendUnconfirmed
		}
		defer control.Close(ctx)
	}
	// Socket closure alone does not prove server exit. Observe the original
	// identity only; never signal a PID that may now belong to another session.
	for {
		exists, e := d.backendProbe(ctx, control, d.pid, d.backendStart)
		if e != nil {
			return errMigrationBackendUnconfirmed
		}
		if !exists {
			return d.failure(err)
		}
		select {
		case <-ctx.Done():
			return errMigrationBackendUnconfirmed
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func migrationBackendExists(ctx context.Context, conn *pgx.Conn, pid uint32, start time.Time) (bool, error) {
	var exists bool
	// An unavailable start time is not evidence that the original session exited.
	err := conn.QueryRow(ctx, `SELECT COALESCE(bool_or(backend_start IS NULL OR backend_start=$2), false)
 FROM pg_stat_activity WHERE pid=$1`, pid, start).Scan(&exists)
	return exists, err
}

func (d *migrationDriver) runCleanup(script string) error {
	locks, ddl, _ := strings.Cut(script, "DO $$")
	locks = strings.Replace(locks, "BEGIN;", "", 1)
	ddl = "DO $$" + strings.TrimSuffix(strings.TrimSpace(ddl), "COMMIT;")
	tx, err := d.conn.Begin(d.ctx)
	if err != nil {
		return d.failure(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(ctx)
	}()
	if _, err = tx.Exec(d.ctx, locks); err == nil {
		// Compute on the executing connection after locking the logical identity.
		err = exposuretarget.SetContext(d.ctx, tx)
	}
	if err == nil {
		_, err = tx.Exec(d.ctx, ddl)
	}
	if err == nil {
		err = tx.Commit(d.ctx)
	}
	return d.failure(err)
}

func (d *migrationDriver) failure(err error) error {
	if err == nil {
		return nil
	}
	if d.ctx.Err() != nil {
		return d.ctx.Err()
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "P0001" {
		return fmt.Errorf("migration refused: %s", pgerr.Message)
	}
	return errors.New("migration database operation failed")
}
