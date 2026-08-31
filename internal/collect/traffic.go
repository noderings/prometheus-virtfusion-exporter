package collect

import (
	"context"
	"time"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog/log"
)

type TrafficCollector struct {
	client     VFClient
	namePrefix string
}

func NewTrafficCollector(client VFClient, namePrefix string) *TrafficCollector {
	return &TrafficCollector{client: client, namePrefix: namePrefix}
}

// Collect fetches monthly traffic for built NR- servers.
func (tc *TrafficCollector) Collect(ctx context.Context, servers []ServerSummary) []*dto.MetricFamily {
	filtered := FilterServers(servers, tc.namePrefix)
	var families []*dto.MetricFamily

	for _, s := range filtered {
		t, err := tc.client.GetServerTraffic(ctx, s.ID)
		if err != nil {
			log.Warn().Err(err).Int("server_id", s.ID).Msg("traffic fetch failed")
			continue
		}

		// total is in bytes; limit is in GB
		families = append(families,
			gaugeFamily(metrics.TrafficUsedBytes, t.Total, s.Name),
			gaugeFamily(metrics.TrafficLimitBytes, t.Limit*gbToBytes, s.Name),
		)
	}

	metrics.ExporterLastSuccess.WithLabelValues("traffic").Set(float64(time.Now().Unix()))
	return families
}
