package collect

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/metrics"
	"github.com/noderings/prometheus-virtfusion-exporter/internal/vfclient"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog/log"
)

// ServerSummary is the subset of fields used from a simple server listing.
type ServerSummary struct {
	ID           int    `json:"id"`
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	HypervisorID int    `json:"hypervisor_id"`
	Status       string `json:"status"`
	Suspended    bool   `json:"suspended"`
	Commissioned int    `json:"commissioned"`
}

func parseServerSummary(raw json.RawMessage) (ServerSummary, error) {
	var extra map[string]any
	if err := json.Unmarshal(raw, &extra); err != nil {
		return ServerSummary{}, err
	}
	s := ServerSummary{
		ID:           anyInt(extra["id"]),
		UUID:         anyString(extra["uuid"]),
		Name:         anyString(extra["name"]),
		HypervisorID: anyInt(extra["hypervisor_id"]),
		Status:       anyString(extra["status"]),
		Suspended:    anyBool(extra["suspended"]),
		Commissioned: anyInt(extra["commissioned"]),
	}
	if s.HypervisorID == 0 {
		s.HypervisorID = anyInt(extra["hypervisorId"])
	}
	if s.HypervisorID == 0 {
		if hyp, ok := extra["hypervisor"].(map[string]any); ok {
			s.HypervisorID = anyInt(hyp["id"])
		}
	}
	if s.Status == "" {
		s.Status = anyString(extra["state"])
	}
	if s.Commissioned == 0 {
		s.Commissioned = anyInt(extra["commissionStatus"])
	}
	return s, nil
}

func anyInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}

func anyBool(v any) bool {
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	case string:
		return strings.EqualFold(n, "true") || n == "1"
	default:
		return false
	}
}

type InventoryCollector struct {
	client     VFClient
	namePrefix string
}

func NewInventoryCollector(client VFClient, namePrefix string) *InventoryCollector {
	return &InventoryCollector{client: client, namePrefix: namePrefix}
}

// Collect runs GET /connect and GET /servers (paginated).
// vf_server_info is emitted only for names matching the configured prefix.
// The full server list is returned so guest/traffic collectors can apply the
// same filter.
func (ic *InventoryCollector) Collect(ctx context.Context) ([]*dto.MetricFamily, []ServerSummary, error) {
	var families []*dto.MetricFamily

	err := ic.client.Connect(ctx)
	if err != nil {
		log.Error().Err(err).Msg("VirtFusion connect failed")
		families = append(families, gaugeFamily(metrics.Up, 0))
		return families, nil, err
	}
	families = append(families, gaugeFamily(metrics.Up, 1))

	rawServers, err := ic.client.ListServers(ctx)
	if err != nil {
		log.Error().Err(err).Msg("list servers failed")
		return families, nil, err
	}

	var servers []ServerSummary
	for _, raw := range rawServers {
		s, err := parseServerSummary(raw)
		if err != nil {
			log.Warn().Err(err).Msg("skip unparseable server")
			continue
		}
		servers = append(servers, s)
	}

	for _, s := range servers {
		if !HasNamePrefix(s.Name, ic.namePrefix) {
			continue
		}
		families = append(families, gaugeFamily(metrics.ServerInfo, 1,
			s.Name,
			strconv.Itoa(s.ID),
			s.UUID,
			strconv.Itoa(s.HypervisorID),
		))
	}

	metrics.ExporterLastSuccess.WithLabelValues("inventory").Set(float64(time.Now().Unix()))

	return families, servers, nil
}

// HasNamePrefix reports whether name starts with prefix (case-insensitive).
// Empty prefix matches everything.
func HasNamePrefix(name, prefix string) bool {
	if prefix == "" {
		return true
	}
	return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
}

// FilterServers returns built NodeRings guests to scrape with remoteState/traffic.
// Stopped guests are included so vf_guest_up can be 0; installing/suspended are not.
func FilterServers(servers []ServerSummary, prefix string) []ServerSummary {
	var out []ServerSummary
	for _, s := range servers {
		if !HasNamePrefix(s.Name, prefix) {
			continue
		}
		if !shouldScrapeGuest(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// shouldScrapeGuest is true for installed guests. VirtFusion simple lists often
// omit power: commissioned=3 / state=complete means built, not running.
func shouldScrapeGuest(s ServerSummary) bool {
	if s.Suspended {
		return false
	}
	st := strings.ToLower(strings.TrimSpace(s.Status))
	switch st {
	case "pending", "allocated", "installing", "suspended":
		return false
	}
	if s.Commissioned > 0 && s.Commissioned < 3 {
		return false
	}
	return true
}

func gaugeFamily(desc *prometheus.Desc, value float64, labelValues ...string) *dto.MetricFamily {
	g := prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labelValues...)
	m := &dto.Metric{}
	_ = g.Write(m)

	fqName := extractName(desc)
	help := extractHelp(desc)
	tp := dto.MetricType_GAUGE
	return &dto.MetricFamily{
		Name:   &fqName,
		Help:   &help,
		Type:   &tp,
		Metric: []*dto.Metric{m},
	}
}

func counterFamily(desc *prometheus.Desc, value float64, labelValues ...string) *dto.MetricFamily {
	c := prometheus.MustNewConstMetric(desc, prometheus.CounterValue, value, labelValues...)
	m := &dto.Metric{}
	_ = c.Write(m)

	fqName := extractName(desc)
	help := extractHelp(desc)
	tp := dto.MetricType_COUNTER
	return &dto.MetricFamily{
		Name:   &fqName,
		Help:   &help,
		Type:   &tp,
		Metric: []*dto.Metric{m},
	}
}

func extractName(desc *prometheus.Desc) string {
	d := &dto.Metric{}
	m := prometheus.MustNewConstMetric(desc, prometheus.UntypedValue, 0, makeLabelPlaceholders(desc)...)
	_ = m.Write(d)

	pd := desc.String()
	// desc.String() is like: Desc{fqName: "vf_up", help: "...", ...}
	start := len("Desc{fqName: \"")
	end := start
	for end < len(pd) && pd[end] != '"' {
		end++
	}
	if end <= len(pd) {
		return pd[start:end]
	}
	return "unknown"
}

func extractHelp(desc *prometheus.Desc) string {
	pd := desc.String()
	const marker = "help: \""
	idx := 0
	for i := 0; i+len(marker) <= len(pd); i++ {
		if pd[i:i+len(marker)] == marker {
			idx = i + len(marker)
			break
		}
	}
	if idx == 0 {
		return ""
	}
	end := idx
	for end < len(pd) && pd[end] != '"' {
		end++
	}
	return pd[idx:end]
}

func makeLabelPlaceholders(desc *prometheus.Desc) []string {
	pd := desc.String()
	const marker = "variableLabels: {"
	idx := 0
	for i := 0; i+len(marker) <= len(pd); i++ {
		if pd[i:i+len(marker)] == marker {
			idx = i + len(marker)
			break
		}
	}
	if idx == 0 {
		return nil
	}
	end := idx
	for end < len(pd) && pd[end] != '}' {
		end++
	}
	if idx == end {
		return nil
	}
	labels := pd[idx:end]
	count := 1
	for _, c := range labels {
		if c == ',' {
			count++
		}
	}
	out := make([]string, count)
	for i := range out {
		out[i] = ""
	}
	return out
}

// VFClient is the subset of vfclient.Client used by collectors.
type VFClient interface {
	Connect(ctx context.Context) error
	ListServers(ctx context.Context) ([]json.RawMessage, error)
	GetServerRemoteState(ctx context.Context, serverID int) (json.RawMessage, error)
	ListHypervisors(ctx context.Context) ([]vfclient.Hypervisor, error)
	GetGroupResources(ctx context.Context, groupID int) ([]vfclient.HypervisorResources, error)
	GetServerTraffic(ctx context.Context, serverID int) (*vfclient.TrafficMonthly, error)
}
