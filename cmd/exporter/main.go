package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/cache"
	"github.com/noderings/prometheus-virtfusion-exporter/internal/collect"
	vfhttp "github.com/noderings/prometheus-virtfusion-exporter/internal/http"
	"github.com/noderings/prometheus-virtfusion-exporter/internal/vfclient"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	var (
		listen           = flag.String("listen", envOr("LISTEN_ADDR", ":9178"), "listen address")
		vfURL            = flag.String("virtfusion-url", os.Getenv("VIRTFUSION_URL"), "VirtFusion control URL (no /api/v1)")
		vfTokenFile      = flag.String("virtfusion-token-file", "", "path to bearer token file")
		refreshInventory = flag.Duration("refresh-inventory", 30*time.Second, "inventory refresh interval")
		refreshCapacity  = flag.Duration("refresh-capacity", 60*time.Second, "capacity refresh interval")
		refreshGuest     = flag.Duration("refresh-guest", 45*time.Second, "guest metrics refresh interval")
		refreshTraffic   = flag.Duration("refresh-traffic", 10*time.Minute, "traffic stats refresh interval")
		guestConcurrency = flag.Int("guest-concurrency", 8, "max concurrent remoteState requests")
		guestTimeout     = flag.Duration("guest-timeout", 25*time.Second, "per-guest remoteState timeout")
		onlyNamePrefix   = flag.String("only-name-prefix", "NR-", "only scrape servers with this name prefix")
		insecureSkipTLS  = flag.Bool("insecure-skip-tls", false, "skip TLS verification (testing only)")
	)
	flag.Parse()

	log.Logger = zerolog.New(os.Stdout).With().
		Timestamp().
		Str("service", "prometheus-virtfusion-exporter").
		Logger()

	token, err := resolveToken(*vfTokenFile)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to resolve VirtFusion token")
	}
	if *vfURL == "" {
		log.Fatal().Msg("--virtfusion-url or VIRTFUSION_URL is required")
	}

	client := vfclient.New(*vfURL, token, *insecureSkipTLS)

	snap := cache.NewSnapshot()

	invCol := collect.NewInventoryCollector(client, *onlyNamePrefix)
	capCol := collect.NewCapacityCollector(client)
	guestCol := collect.NewGuestCollector(client, *guestConcurrency, *guestTimeout, *onlyNamePrefix)
	trafficCol := collect.NewTrafficCollector(client, *onlyNamePrefix)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Shared server list updated by inventory ticker, read by guest/traffic tickers.
	var lastServers []collect.ServerSummary
	var serversMu sync.Mutex
	var inventoryOK atomic.Bool

	runInventory := func() {
		families, servers, err := invCol.Collect(ctx)
		if err != nil {
			log.Error().Err(err).Msg("inventory collection failed")
			inventoryOK.Store(false)
		} else {
			inventoryOK.Store(true)
			serversMu.Lock()
			lastServers = servers
			serversMu.Unlock()
		}
		mergeSnapshot(snap, "inventory", families)
	}

	runCapacity := func() {
		families, err := capCol.Collect(ctx)
		if err != nil {
			log.Error().Err(err).Msg("capacity collection failed")
		}
		mergeSnapshot(snap, "capacity", families)
	}

	runGuest := func() {
		if !inventoryOK.Load() {
			log.Warn().Msg("skip guest scrape: last inventory failed")
			return
		}
		serversMu.Lock()
		srv := lastServers
		serversMu.Unlock()
		families := guestCol.Collect(ctx, srv)
		mergeSnapshot(snap, "guest", families)
	}

	runTraffic := func() {
		if !inventoryOK.Load() {
			log.Warn().Msg("skip traffic scrape: last inventory failed")
			return
		}
		serversMu.Lock()
		srv := lastServers
		serversMu.Unlock()
		families := trafficCol.Collect(ctx, srv)
		mergeSnapshot(snap, "traffic", families)
	}

	// Initial collection before serving.
	runInventory()
	runCapacity()
	runGuest()
	runTraffic()

	go ticker(ctx, *refreshInventory, runInventory)
	go ticker(ctx, *refreshCapacity, runCapacity)
	go ticker(ctx, *refreshGuest, runGuest)
	go ticker(ctx, *refreshTraffic, runTraffic)

	mux := vfhttp.NewMux(snap)
	srv := &http.Server{
		Addr:         *listen,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info().Str("addr", *listen).Msg("starting exporter")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("http server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down")
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error().Err(err).Msg("server shutdown error")
	}
	log.Info().Msg("exporter stopped")
}

func resolveToken(tokenFile string) (string, error) {
	if tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	token := os.Getenv("VIRTFUSION_TOKEN")
	if token == "" {
		return "", fmt.Errorf("set --virtfusion-token-file or VIRTFUSION_TOKEN")
	}
	return token, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func ticker(ctx context.Context, interval time.Duration, fn func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// snapshotParts holds the last families per collector. The merged snapshot is
// rebuilt on each update so /metrics sees a consistent view.
var (
	snapshotParts   = map[string][]*dto.MetricFamily{}
	snapshotPartsMu sync.Mutex
)

func mergeSnapshot(snap *cache.Snapshot, name string, families []*dto.MetricFamily) {
	snapshotPartsMu.Lock()
	defer snapshotPartsMu.Unlock()

	snapshotParts[name] = families

	var merged []*dto.MetricFamily
	for _, fams := range snapshotParts {
		merged = append(merged, fams...)
	}
	snap.Update(merged)
}
