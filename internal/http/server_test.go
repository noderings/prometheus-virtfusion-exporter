package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/cache"
	vfhttp "github.com/noderings/prometheus-virtfusion-exporter/internal/http"
	"github.com/noderings/prometheus-virtfusion-exporter/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestMetricsHandler_NoHTTPClient verifies that GET /metrics only reads the
// in-memory snapshot and never calls the VirtFusion HTTP API.
// The test constructs a snapshot directly — no VFClient is involved.
func TestMetricsHandler_NoHTTPClient(t *testing.T) {
	snap := cache.NewSnapshot()

	// Build a fake metric family manually.
	name := "vf_up"
	help := "VirtFusion API reachable via GET /connect"
	tp := dto.MetricType_GAUGE
	val := float64(1)
	snap.Update([]*dto.MetricFamily{
		{
			Name: &name,
			Help: &help,
			Type: &tp,
			Metric: []*dto.Metric{
				{Gauge: &dto.Gauge{Value: &val}},
			},
		},
	})

	mux := vfhttp.NewMux(snap)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, "vf_up") {
		t.Error("expected vf_up in output")
	}

	// Verify no pve_* series are present.
	if strings.Contains(text, "pve_") {
		t.Error("must not contain pve_* series")
	}
}

func TestMetricsHandler_ContainsProcessMetrics(t *testing.T) {
	snap := cache.NewSnapshot()
	mux := vfhttp.NewMux(snap)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, "process_") && !strings.Contains(text, "go_") {
		t.Error("expected process or go metrics from default gatherer")
	}
}

func TestHealthzHandler(t *testing.T) {
	snap := cache.NewSnapshot()
	mux := vfhttp.NewMux(snap)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ok") {
		t.Error("expected 'ok' in healthz response")
	}
}

func TestMetricsHandler_VFMetricsPresent(t *testing.T) {
	snap := cache.NewSnapshot()

	// Simulate a full snapshot with vf_up and vf_hypervisor_up.
	upGauge := prometheus.MustNewConstMetric(metrics.Up, prometheus.GaugeValue, 1)
	upMetric := &dto.Metric{}
	_ = upGauge.Write(upMetric)

	hvGauge := prometheus.MustNewConstMetric(metrics.HypervisorUp, prometheus.GaugeValue, 1, "1", "10", "hv1")
	hvMetric := &dto.Metric{}
	_ = hvGauge.Write(hvMetric)

	upName := "vf_up"
	upHelp := "test"
	hvName := "vf_hypervisor_up"
	hvHelp := "test"
	gType := dto.MetricType_GAUGE

	snap.Update([]*dto.MetricFamily{
		{Name: &upName, Help: &upHelp, Type: &gType, Metric: []*dto.Metric{upMetric}},
		{Name: &hvName, Help: &hvHelp, Type: &gType, Metric: []*dto.Metric{hvMetric}},
	})

	mux := vfhttp.NewMux(snap)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, want := range []string{"vf_up", "vf_hypervisor_up"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %s in output", want)
		}
	}
}
