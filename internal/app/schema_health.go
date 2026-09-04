package app

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/migrations"
)

// undefinedTable is PostgreSQL's SQLSTATE for a relation that does not exist.
const undefinedTable = "42P01"

func pgIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == undefinedTable
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// checkedModules are the migration modules this binary reads tables from.
//
// bot is deliberately absent. Its tree ships 000009, the retirement migration,
// but the safe runner stops at 000008 and only the protected destructive
// workflow goes further — so on a correctly deployed database bot sits one
// version below the tree by design, and checking it would report every healthy
// deployment as broken. Nothing in this binary reads the bot schema; that is
// what 000009 retires.
var checkedModules = []string{"user", "post", "notification", "settings"}

// appliedMigration is one row of a schema_migrations_<module> table.
type appliedMigration struct {
	version int
	dirty   bool
}

// pendingModules names the modules whose database state is behind the migrations
// compiled into this binary, in a stable order so the health reason does not
// change between reads.
//
// Dirty counts as behind: golang-migrate marks a module dirty when a migration
// failed part-way, and the schema is then in neither the old shape nor the new
// one. A version ahead of this build does not count — that is a rollback, and
// migrations here are additive, so the older binary still reads what it knows.
func pendingModules(latest map[string]int, applied map[string]appliedMigration) []string {
	var pending []string
	for module, want := range latest {
		got, ok := applied[module]
		if !ok || got.dirty || got.version < want {
			pending = append(pending, module)
		}
	}
	sort.Strings(pending)
	return pending
}

// migrationVersionProbe answers whether the connected database has caught up
// with the migrations shipped in this binary.
type migrationVersionProbe struct {
	pool   *pgxpool.Pool
	latest map[string]int
}

func newMigrationVersionProbe(pool *pgxpool.Pool) (*migrationVersionProbe, error) {
	all, err := migrations.LatestVersions(migrations.Embedded())
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	latest := make(map[string]int, len(checkedModules))
	for _, module := range checkedModules {
		version, ok := all[module]
		if !ok {
			return nil, fmt.Errorf("no migrations embedded for module %q", module)
		}
		latest[module] = version
	}

	return &migrationVersionProbe{pool: pool, latest: latest}, nil
}

// PendingModules reads each module's applied version and compares it.
//
// A missing schema_migrations_<module> table is not an error: it is what a
// database that never ran that module's migrations looks like, which is the
// state this probe exists to report.
func (p *migrationVersionProbe) PendingModules(ctx context.Context) ([]string, error) {
	applied := make(map[string]appliedMigration, len(p.latest))

	for module := range p.latest {
		// The table name cannot be a bind parameter, and module comes from
		// checkedModules — a package-level list of constants, never from input.
		query := fmt.Sprintf("SELECT version, dirty FROM %s", pgIdentifier("schema_migrations_"+module))

		var row appliedMigration
		err := p.pool.QueryRow(ctx, query).Scan(&row.version, &row.dirty)
		switch {
		case err == nil:
			applied[module] = row
		case isUndefinedTable(err), isNoRows(err):
			// Absent table or empty table: nothing has been applied.
		default:
			return nil, fmt.Errorf("read %s migration version: %w", module, err)
		}
	}

	return pendingModules(p.latest, applied), nil
}
