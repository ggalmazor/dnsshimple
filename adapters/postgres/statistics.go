package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"dnsshimple/core/admin"
	"dnsshimple/kit"
)

// Statistics implements admin.Statistics from Postgres catalog views. It
// deliberately reads pg_catalog only - row counts are the planner's
// live-tuple estimates - so no component's tables are queried and the
// ownership rule holds.
type Statistics struct {
	pool *pgxpool.Pool
}

func NewStatistics(pool *pgxpool.Pool) Statistics {
	return Statistics{pool: pool}
}

var _ admin.Statistics = Statistics{}

func (statistics Statistics) DatabaseBytes(ctx context.Context) kit.Outcome[int64] {
	var size int64
	err := statistics.pool.QueryRow(ctx,
		`SELECT pg_database_size(current_database())`).Scan(&size)
	if err != nil {
		return kit.Failure[int64](fmt.Errorf("sizing the database: %w", err))
	}
	return kit.Success(size)
}

// Maintenance implements admin.Maintenance: whole-database upkeep,
// operating on tables as tables, never on any component's rows.
type Maintenance struct {
	pool *pgxpool.Pool
}

func NewMaintenance(pool *pgxpool.Pool) Maintenance {
	return Maintenance{pool: pool}
}

var _ admin.Maintenance = Maintenance{}

func (maintenance Maintenance) VacuumAnalyze(ctx context.Context) kit.Outcome[kit.Nothing] {
	// Plain VACUUM, never FULL: it runs alongside the live server without
	// taking exclusive locks. ANALYZE refreshes the planner estimates.
	if _, err := maintenance.pool.Exec(ctx, `VACUUM (ANALYZE)`); err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("vacuuming the database: %w", err))
	}
	return kit.Done()
}

func (statistics Statistics) Tables(ctx context.Context) kit.Outcome[[]admin.TableStat] {
	rows, err := statistics.pool.Query(ctx, `
		SELECT relname, n_live_tup, pg_total_relation_size(relid)
		FROM pg_stat_user_tables
		WHERE schemaname = current_schema()
		ORDER BY relname`)
	if err != nil {
		return kit.Failure[[]admin.TableStat](fmt.Errorf("listing table statistics: %w", err))
	}
	defer rows.Close()
	var stats []admin.TableStat
	for rows.Next() {
		var stat admin.TableStat
		if err := rows.Scan(&stat.Name, &stat.LiveRows, &stat.TotalBytes); err != nil {
			return kit.Failure[[]admin.TableStat](fmt.Errorf("scanning a table stat: %w", err))
		}
		stats = append(stats, stat)
	}
	return kit.Success(stats)
}
