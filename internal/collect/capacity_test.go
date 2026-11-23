package collect_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/collect"
	"github.com/noderings/prometheus-virtfusion-exporter/internal/vfclient"
	dto "github.com/prometheus/client_model/go"
)

type stubVFClient struct {
	hypervisors []vfclient.Hypervisor
	resources   map[int][]vfclient.HypervisorResources
	groupCalls  int
}

func (s *stubVFClient) Connect(context.Context) error { return nil }
func (s *stubVFClient) ListServers(context.Context) ([]json.RawMessage, error) {
	return nil, nil
}
func (s *stubVFClient) GetServerRemoteState(context.Context, int) (json.RawMessage, error) {
	return nil, nil
}
func (s *stubVFClient) ListHypervisors(context.Context) ([]vfclient.Hypervisor, error) {
	return s.hypervisors, nil
}
func (s *stubVFClient) GetGroupResources(_ context.Context, groupID int) ([]vfclient.HypervisorResources, error) {
	s.groupCalls++
	return s.resources[groupID], nil
}
func (s *stubVFClient) GetServerTraffic(context.Context, int) (*vfclient.TrafficMonthly, error) {
	return nil, nil
}

func familyNamed(families []*dto.MetricFamily, name string) []*dto.MetricFamily {
	var out []*dto.MetricFamily
	for _, f := range families {
		if f.GetName() == name {
			out = append(out, f)
		}
	}
	return out
}

func labelMap(m *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, l := range m.Label {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

func TestCapacityCollector_OneResourcesCallPerGroup(t *testing.T) {
	stub := &stubVFClient{
		hypervisors: []vfclient.Hypervisor{
			{ID: 11, Name: "hv1", GroupID: 3, Enabled: true},
			{ID: 12, Name: "hv2", GroupID: 3, Enabled: true},
		},
		resources: map[int][]vfclient.HypervisorResources{
			3: {
				mustHVResources(t, 11, "hv1", 8, 32, 4096, 16384, 100, 500, 20, 100),
				mustHVResources(t, 12, "hv2", 4, 16, 2048, 8192, 50, 200, 0, 0),
			},
		},
	}

	cc := collect.NewCapacityCollector(stub)
	families, err := cc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if stub.groupCalls != 1 {
		t.Fatalf("expected one group resources call, got %d", stub.groupCalls)
	}

	cpuAlloc := familyNamed(families, "vf_cpu_allocated")
	if len(cpuAlloc) != 2 {
		t.Fatalf("expected 2 vf_cpu_allocated series, got %d", len(cpuAlloc))
	}

	byHost := map[string]float64{}
	for _, f := range cpuAlloc {
		m := f.Metric[0]
		labels := labelMap(m)
		if labels["group_id"] != "3" {
			t.Errorf("expected group_id=3, got %q", labels["group_id"])
		}
		byHost[labels["hypervisor_id"]] = m.Gauge.GetValue()
	}
	if byHost["11"] != 8 || byHost["12"] != 4 {
		t.Fatalf("unexpected allocated CPU by host: %+v", byHost)
	}

	diskAlloc := familyNamed(families, "vf_disk_allocated_bytes")
	gotDisk := map[string]float64{}
	for _, f := range diskAlloc {
		m := f.Metric[0]
		gotDisk[labelMap(m)["hypervisor_id"]] = m.Gauge.GetValue()
	}
	const gb = 1024 * 1024 * 1024
	if gotDisk["11"] != 120*gb {
		t.Errorf("hv1 disk allocated: got %v want %v (local+other)", gotDisk["11"], 120*gb)
	}
}

func mustHVResources(t *testing.T, id int, name string, cpuA, cpuM, memA, memM, diskA, diskM, otherA, otherM float64) vfclient.HypervisorResources {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"hypervisor": map[string]any{"id": id, "name": name, "enabled": true},
		"resources": map[string]any{
			"cpuCores":     map[string]any{"allocated": cpuA, "max": cpuM},
			"memory":       map[string]any{"allocated": memA, "max": memM},
			"localStorage": map[string]any{"allocated": diskA, "max": diskM},
			"otherStorage": []map[string]any{{"allocated": otherA, "max": otherM}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var row vfclient.HypervisorResources
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	return row
}
