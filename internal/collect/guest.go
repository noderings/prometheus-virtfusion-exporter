package collect

import (
	"context"
	"sync"
	"time"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog/log"
)

type GuestCollector struct {
	client      VFClient
	concurrency int
	timeout     time.Duration
	namePrefix  string
}

func NewGuestCollector(client VFClient, concurrency int, timeout time.Duration, namePrefix string) *GuestCollector {
	return &GuestCollector{
		client:      client,
		concurrency: concurrency,
		timeout:     timeout,
		namePrefix:  namePrefix,
	}
}

// Collect fetches remoteState for running NR- servers concurrently.
func (gc *GuestCollector) Collect(ctx context.Context, servers []ServerSummary) []*dto.MetricFamily {
	start := time.Now()
	defer func() {
		metrics.GuestScrapeSeconds.Observe(time.Since(start).Seconds())
	}()

	filtered := FilterServers(servers, gc.namePrefix)
	if len(filtered) == 0 {
		return nil
	}

	type result struct {
		families []*dto.MetricFamily
	}

	results := make([]result, len(filtered))
	sem := make(chan struct{}, gc.concurrency)
	var wg sync.WaitGroup

	for i, s := range filtered {
		wg.Add(1)
		go func(idx int, srv ServerSummary) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			callCtx, cancel := context.WithTimeout(ctx, gc.timeout)
			defer cancel()

			raw, err := gc.client.GetServerRemoteState(callCtx, srv.ID)
			if err != nil {
				log.Warn().Err(err).Int("server_id", srv.ID).Str("name", srv.Name).Msg("remoteState failed")
				metrics.RemoteStateErrorsTotal.WithLabelValues("fetch").Inc()
				return
			}

			families, err := MapRemoteState(raw)
			if err != nil {
				log.Warn().Err(err).Int("server_id", srv.ID).Msg("mapRemoteState failed")
				metrics.RemoteStateErrorsTotal.WithLabelValues("parse").Inc()
				return
			}

			results[idx] = result{families: families}
		}(i, s)
	}

	wg.Wait()

	var allFamilies []*dto.MetricFamily
	for _, r := range results {
		allFamilies = append(allFamilies, r.families...)
	}

	metrics.ExporterLastSuccess.WithLabelValues("guest").Set(float64(time.Now().Unix()))
	return allFamilies
}
