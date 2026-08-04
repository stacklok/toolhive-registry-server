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
       COUNT(re.id) FILTER (WHERE re.entry_type = 'SKILL')::bigint AS skill_count,
       COUNT(re.id) FILTER (WHERE re.entry_type = 'PLUGIN')::bigint AS plugin_count
  FROM source src
  LEFT JOIN registry_entry re ON re.source_id = src.id
 GROUP BY src.name
 ORDER BY src.name`

// registryMetricCountsQueryTimeout bounds the full-table aggregate the
// observable-gauge callback runs. The scrape handler's own Timeout can't
// reach this query — the OTel Prometheus exporter builds its own
// context.TODO() rather than propagating the request context — so this is
// the only place that can actually cancel a stalled query.
//
// It sits above the caching reader's negative TTL but well below
// PeriodicReader's 30s collect timeout, so a stalled query fails on this
// deadline rather than taking the whole export down with it.
const registryMetricCountsQueryTimeout = 20 * time.Second

// registryMetricCountsContext caps ctx at registryMetricCountsQueryTimeout. It
// is an upper bound only: a caller that already has a tighter deadline keeps
// it. The cancel func is returned for the caller to defer.
func registryMetricCountsContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, registryMetricCountsQueryTimeout)
}

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
	ctx, cancel := registryMetricCountsContext(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx, registryMetricCountsQuery)
	if err != nil {
		return nil, fmt.Errorf("query registry metric counts: %w", err)
	}
	defer rows.Close()

	var counts []telemetry.RegistryMetricCount
	for rows.Next() {
		var count telemetry.RegistryMetricCount
		if err := rows.Scan(&count.SourceName, &count.ServerCount, &count.SkillCount, &count.PluginCount); err != nil {
			return nil, fmt.Errorf("scan registry metric count: %w", err)
		}
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registry metric counts: %w", err)
	}

	return counts, nil
}
