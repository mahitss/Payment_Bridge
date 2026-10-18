package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.up.sql
var MigrationsFS embed.FS

// RunMigrations applies all embedded .up.sql schema migrations sequentially in ascending filename order.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := MigrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	for _, filename := range files {
		content, err := MigrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", filename, err)
		}

		if _, err := pool.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("failed executing migration %s: %w", filename, err)
		}
	}

	return nil
}

// RunMigrations on PostgresRepository invokes the migration runner on its pool.
func (r *PostgresRepository) RunMigrations(ctx context.Context) error {
	return RunMigrations(ctx, r.pool)
}
