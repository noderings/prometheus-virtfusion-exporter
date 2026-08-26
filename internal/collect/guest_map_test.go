package collect_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/collect"
	dto "github.com/prometheus/client_model/go"
)

// fixtureRemoteState is a representative VirtFusion remoteState response.
var fixtureRemoteState = `{
	"id": 42,
	"uuid": "abc-def-123",
	"name": "NR-test-vm",
	"remoteState": {
		"cpuUsage": 55.2,
		"memory": {
			"used": 536870912,
			"size": 1073741824
		},
		"disk": {
			"read": 1048576,
			"write": 2097152
		},
		"net": {
			"rx": 500000,
			"tx": 300000
		},
		"agent": {
			"fsinfo": [
				{
					"mountpoint": "/",
					"used-bytes": 5368709120,
					"total-bytes": 21474836480
				},
				{
					"mountpoint": "/boot",
					"used-bytes": 104857600,
					"total-bytes": 524288000
				}
			]
		}
	}
}`

func familyValue(f *dto.MetricFamily) float64 {
	if len(f.Metric) == 0 {
		return 0
	}
	m := f.Metric[0]
	if m.Gauge != nil {
		return m.Gauge.GetValue()
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return 0
}

func TestMapRemoteState_FullFixture(t *testing.T) {
	families, err := collect.MapRemoteState(json.RawMessage(fixtureRemoteState))
	if err != nil {
		t.Fatalf("MapRemoteState: %v", err)
	}

	expected := map[string]float64{
		"vf_cpu_usage_ratio":        0.552,
		"vf_memory_usage_bytes":     536870912,
		"vf_memory_size_bytes":      1073741824,
		"vf_disk_read_bytes":        1048576,
		"vf_disk_write_bytes":       2097152,
		"vf_network_receive_bytes":  500000,
		"vf_network_transmit_bytes": 300000,
	}

	found := map[string]bool{}
	for _, f := range families {
		name := f.GetName()
		if v, ok := expected[name]; ok {
			actual := familyValue(f)
			if math.Abs(actual-v) > 0.001 {
				t.Errorf("%s: expected %f, got %f", name, v, actual)
			}
			found[name] = true
		}
	}

	for name := range expected {
		if !found[name] {
			t.Errorf("missing expected metric: %s", name)
		}
	}

	fsCount := 0
	for _, f := range families {
		if f.GetName() == "vf_fs_used_bytes" || f.GetName() == "vf_fs_size_bytes" {
			fsCount++
		}
	}
	if fsCount != 4 {
		t.Errorf("expected 4 FS metrics (2 mounts × 2 metrics), got %d", fsCount)
	}
}

func TestMapRemoteState_CPUPercent(t *testing.T) {
	raw := `{"id":1,"uuid":"u","name":"n","remoteState":{"cpuUsage":75}}`
	families, err := collect.MapRemoteState(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "vf_cpu_usage_ratio" {
			v := familyValue(f)
			if math.Abs(v-0.75) > 0.001 {
				t.Errorf("expected 0.75, got %f", v)
			}
			return
		}
	}
	t.Error("vf_cpu_usage_ratio not found")
}

func TestMapRemoteState_CPUAlreadyRatio(t *testing.T) {
	raw := `{"id":1,"uuid":"u","name":"n","remoteState":{"cpuUsage":0.42}}`
	families, err := collect.MapRemoteState(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "vf_cpu_usage_ratio" {
			v := familyValue(f)
			if math.Abs(v-0.42) > 0.001 {
				t.Errorf("expected 0.42, got %f", v)
			}
			return
		}
	}
	t.Error("vf_cpu_usage_ratio not found")
}

func TestMapRemoteState_MissingKeys(t *testing.T) {
	raw := `{"id":1,"uuid":"u","name":"n","remoteState":{}}`
	families, err := collect.MapRemoteState(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 0 {
		t.Errorf("expected 0 families for empty remoteState, got %d", len(families))
	}
}

func TestMapRemoteState_NoRemoteState(t *testing.T) {
	raw := `{"id":1,"uuid":"u","name":"n"}`
	families, err := collect.MapRemoteState(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 0 {
		t.Errorf("expected 0 families for absent remoteState, got %d", len(families))
	}
}

func TestMapRemoteState_LabVirtFusion7Shape(t *testing.T) {
	raw := `{
		"id": 1,
		"uuid": "7dabfecf-d446-405a-ba32-72eda795e3a9",
		"name": "NR-test",
		"remoteState": {
			"cpu": 0.8,
			"memory": {"memtotal": "948384", "memavailable": "600600", "_source": "agent_meminfo"},
			"disk": {"vda": {"rd.bytes": "1000", "wr.bytes": "2000"}},
			"network": {"eth0": {"rx.bytes": "3000", "tx.bytes": "4000"}},
			"agent": {"fsinfo": [{"mountpoint": "/", "used-bytes": 10, "total-bytes": 20}]}
		}
	}`
	families, err := collect.MapRemoteState(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, f := range families {
		got[f.GetName()] = familyValue(f)
	}
	if math.Abs(got["vf_cpu_usage_ratio"]-0.008) > 0.0001 {
		t.Errorf("cpu ratio: got %v want 0.008", got["vf_cpu_usage_ratio"])
	}
	wantMemSize := 948384.0 * 1024
	wantMemUsed := (948384.0 - 600600.0) * 1024
	if math.Abs(got["vf_memory_size_bytes"]-wantMemSize) > 1 {
		t.Errorf("mem size: got %v want %v", got["vf_memory_size_bytes"], wantMemSize)
	}
	if math.Abs(got["vf_memory_usage_bytes"]-wantMemUsed) > 1 {
		t.Errorf("mem used: got %v want %v", got["vf_memory_usage_bytes"], wantMemUsed)
	}
	if got["vf_disk_read_bytes"] != 1000 || got["vf_disk_write_bytes"] != 2000 {
		t.Errorf("disk: %+v", got)
	}
	if got["vf_network_receive_bytes"] != 3000 || got["vf_network_transmit_bytes"] != 4000 {
		t.Errorf("net: %+v", got)
	}
	if got["vf_fs_used_bytes"] != 10 {
		t.Errorf("fs used: %v", got["vf_fs_used_bytes"])
	}
}

func TestMapRemoteState_FSInfoBoolIsIgnored(t *testing.T) {
	raw := `{"id":1,"uuid":"u","name":"n","remoteState":{"cpu":12,"agent":{"fsinfo":false}}}`
	families, err := collect.MapRemoteState(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if strings.HasPrefix(f.GetName(), "vf_fs_") {
			t.Fatalf("fs metric emitted when fsinfo is bool: %s", f.GetName())
		}
	}
}

func TestMapRemoteState_OmitNICIfUnknown(t *testing.T) {
	raw := `{"id":1,"uuid":"u","name":"n","remoteState":{"cpuUsage":10,"memory":{"used":100,"size":200}}}`
	families, err := collect.MapRemoteState(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		name := f.GetName()
		if name == "vf_network_receive_bytes" || name == "vf_network_transmit_bytes" {
			t.Errorf("NIC counter %s should be omitted when net is not present", name)
		}
	}
}
