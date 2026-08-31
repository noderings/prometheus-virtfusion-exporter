package collect

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

// RemoteStateData is the partial structure of GET /servers/{id}?remoteState=true.
// VirtFusion 7.x lab uses cpu / memtotal / disk.<dev>["rd.bytes"] / network.<iface>["rx.bytes"].
// Older docs/fixtures used cpuUsage / memory.used / disk.read / net.rx. Map both.
type RemoteStateData struct {
	ID   int    `json:"id"`
	UUID string `json:"uuid"`
	Name string `json:"name"`

	RemoteState json.RawMessage `json:"remoteState"`
}

type FSInfoEntry struct {
	Mount string  `json:"mountpoint"`
	Used  float64 `json:"used-bytes"`
	Size  float64 `json:"total-bytes"`
}

func anyFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func mapFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f, ok := anyFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func asObject(raw json.RawMessage) map[string]any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func cpuRatio(rs map[string]any) (float64, bool) {
	// OpenAPI-style cpuUsage: 0–1 ratio or 0–100 percent.
	if v, ok := mapFloat(rs, "cpuUsage"); ok {
		if v > 1 {
			v = v / 100.0
		}
		return v, true
	}
	// VirtFusion 7.x remoteState.cpu is libvirt percent (idle guests often 0.8).
	if v, ok := mapFloat(rs, "cpu"); ok {
		return v / 100.0, true
	}
	return 0, false
}

func memoryBytes(rs map[string]any) (used, size float64, ok bool) {
	mem, _ := rs["memory"].(map[string]any)
	if mem == nil {
		return 0, 0, false
	}
	if u, uok := mapFloat(mem, "used"); uok {
		s, _ := mapFloat(mem, "size")
		return u, s, true
	}
	// qemu-guest-agent meminfo is kB strings (memtotal / memavailable).
	totalKB, tok := mapFloat(mem, "memtotal")
	availKB, aok := mapFloat(mem, "memavailable")
	if !tok {
		return 0, 0, false
	}
	size = totalKB * 1024
	if aok {
		used = (totalKB - availKB) * 1024
		if used < 0 {
			used = 0
		}
	}
	return used, size, true
}

func sumNested(rs map[string]any, parent string, valueKeys ...string) (float64, bool) {
	block, _ := rs[parent].(map[string]any)
	if block == nil {
		return 0, false
	}
	var total float64
	found := false
	for _, v := range block {
		inner, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if f, ok := mapFloat(inner, valueKeys...); ok {
			total += f
			found = true
		}
	}
	return total, found
}

func diskBytes(rs map[string]any) (read, write float64, ok bool) {
	if disk, _ := rs["disk"].(map[string]any); disk != nil {
		if r, rok := mapFloat(disk, "read"); rok {
			w, _ := mapFloat(disk, "write")
			return r, w, true
		}
	}
	r, rok := sumNested(rs, "disk", "rd.bytes", "rd_bytes")
	w, wok := sumNested(rs, "disk", "wr.bytes", "wr_bytes")
	return r, w, rok || wok
}

func netBytes(rs map[string]any) (rx, tx float64, ok bool) {
	if net, _ := rs["net"].(map[string]any); net != nil {
		if r, rok := mapFloat(net, "rx"); rok {
			t, _ := mapFloat(net, "tx")
			return r, t, true
		}
	}
	r, rok := sumNested(rs, "network", "rx.bytes", "rx_bytes")
	t, tok := sumNested(rs, "network", "tx.bytes", "tx_bytes")
	return r, t, rok || tok
}

func parseFSInfo(agent map[string]any) []FSInfoEntry {
	raw, ok := agent["fsinfo"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]FSInfoEntry, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			mount, _ := m["mountpoint"].(string)
			used, _ := mapFloat(m, "used-bytes")
			size, _ := mapFloat(m, "total-bytes")
			if mount == "" {
				continue
			}
			out = append(out, FSInfoEntry{Mount: mount, Used: used, Size: size})
		}
		return out
	default:
		// qemu-guest-agent down: fsinfo is often `false`.
		return nil
	}
}

func unwrapServerPayload(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '{' {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &envelope); err == nil && len(bytes.TrimSpace(envelope.Data)) > 2 {
			return envelope.Data
		}
	}
	return raw
}

// RemoteStateLooksLive reports whether GET /servers/{id}?remoteState=true has
// hypervisor stats. Empty / false / missing remoteState means the guest is off.
// VirtFusion simple lists do not expose power; commissioned=3 is install-complete.
func RemoteStateLooksLive(raw json.RawMessage) bool {
	raw = unwrapServerPayload(raw)
	var d RemoteStateData
	if err := json.Unmarshal(raw, &d); err != nil {
		return false
	}
	rs := asObject(d.RemoteState)
	if len(rs) == 0 {
		return false
	}
	if _, ok := cpuRatio(rs); ok {
		return true
	}
	if _, _, ok := memoryBytes(rs); ok {
		return true
	}
	if _, _, ok := diskBytes(rs); ok {
		return true
	}
	if _, _, ok := netBytes(rs); ok {
		return true
	}
	return false
}

// MapRemoteState maps a raw VirtFusion remoteState JSON to Prometheus metric families.
// Tolerates missing keys — omits metrics for absent data rather than guessing.
func MapRemoteState(raw json.RawMessage) ([]*dto.MetricFamily, error) {
	raw = unwrapServerPayload(raw)

	var d RemoteStateData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}

	sid := strconv.Itoa(d.ID)
	labels := []string{d.Name, sid, d.UUID}
	var families []*dto.MetricFamily

	rs := asObject(d.RemoteState)
	if rs == nil {
		return families, nil
	}

	if v, ok := cpuRatio(rs); ok {
		families = append(families, gaugeFamily(metrics.CPUUsageRatio, v, labels...))
	}

	if used, size, ok := memoryBytes(rs); ok {
		families = append(families, gaugeFamily(metrics.MemoryUsageBytes, used, labels...))
		if size > 0 {
			families = append(families, gaugeFamily(metrics.MemorySizeBytes, size, labels...))
		}
	}

	if read, write, ok := diskBytes(rs); ok {
		families = append(families, counterFamily(metrics.DiskReadBytes, read, labels...))
		families = append(families, counterFamily(metrics.DiskWriteBytes, write, labels...))
	}

	if rx, tx, ok := netBytes(rs); ok {
		families = append(families, counterFamily(metrics.NetworkReceiveBytes, rx, labels...))
		families = append(families, counterFamily(metrics.NetworkTransmitBytes, tx, labels...))
	}

	if agent, _ := rs["agent"].(map[string]any); agent != nil {
		for _, fs := range parseFSInfo(agent) {
			fsLabels := append(labels, fs.Mount) //nolint:gocritic
			families = append(families, gaugeFamily(metrics.FSUsedBytes, fs.Used, fsLabels...))
			families = append(families, gaugeFamily(metrics.FSSizeBytes, fs.Size, fsLabels...))
		}
	}

	return families, nil
}
