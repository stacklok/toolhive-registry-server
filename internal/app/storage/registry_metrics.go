package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
)

const registryMetricCountsQuery = `
SELECT src.name,
       COUNT(re.id) FILTER (WHERE re.entry_type = 'MCP')::bigint AS server_count,
       COUNT(re.id) FILTER (WHERE re.entry_type = 'SKILL')::bigint AS skill_count
  FROM source src
  LEFT JOIN registry_entry re ON re.source_id = src.id
 GROUP BY src.name
 ORDER BY src.name`

// registryMetricCountsQueryTimeout bounds the full-table aggregate the
// observable-gauge callback runs. The scrape handler's own Timeout can't
// reach this query — the OTel Prometheus exporter builds its own
// context.TODO() rather than propagating the request context — so this is
// the only place that can actually cancel a stalled query.
const registryMetricCountsQueryTimeout = 5 * time.Second

type registryMetricsReader struct {
	pool *pgxpool.Pool
}

var _ telemetry.RegistryMetricReader = (*registryMetricsReader)(nil)

// CreateRegistryMetricsReader creates a reader for source-level registry metrics.
func (d *DatabaseFactory) CreateRegistryMetricsReader(_ context.Context) (telemetry.RegistryMetricReader, error) {
	if d.pool == nil {
		return nil, fmt.Errorf("pgx pool is required")
	}

	return &registryMetricsReader{pool: d.pool}, nil
}

func (r *registryMetricsReader) RegistryMetricCounts(
	ctx context.Context,
) ([]telemetry.RegistryMetricCount, error) {
	ctx, cancel := context.WithTimeout(ctx, registryMetricCountsQueryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, registryMetricCountsQuery)
	if err != nil {
		return nil, fmt.Errorf("query registry metric counts: %w", err)
	}
	defer rows.Close()

	var counts []telemetry.RegistryMetricCount
	for rows.Next() {
		var count telemetry.RegistryMetricCount
		if err := rows.Scan(&count.SourceName, &count.ServerCount, &count.SkillCount); err != nil {
			return nil, fmt.Errorf("scan registry metric count: %w", err)
		}
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registry metric counts: %w", err)
	}

	return counts, nil
}
