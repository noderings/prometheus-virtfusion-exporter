package collect_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/collect"
	"github.com/noderings/prometheus-virtfusion-exporter/internal/vfclient"
)

func TestFilterServers_PrefixAndStatus(t *testing.T) {
	servers := []collect.ServerSummary{
		{ID: 1, Name: "NR-abc", Status: "running"},
		{ID: 2, Name: "NR-def", Status: "stopped"},
		{ID: 3, Name: "other-vm", Status: "running"},
		{ID: 4, Name: "NR-ghi", Status: "active"},
		{ID: 5, Name: "nr-lowercase", Status: "running"},
		{ID: 6, Name: "NR-jkl", Status: "suspended"},
	}

	filtered := collect.FilterServers(servers, "NR-")

	if len(filtered) != 4 {
		t.Fatalf("expected 4 servers (include stopped), got %d: %+v", len(filtered), filtered)
	}

	names := map[string]bool{}
	for _, s := range filtered {
		names[s.Name] = true
	}

	if !names["NR-abc"] {
		t.Error("expected NR-abc")
	}
	if !names["NR-def"] {
		t.Error("expected NR-def (stopped still scraped so vf_guest_up can be 0)")
	}
	if !names["NR-ghi"] {
		t.Error("expected NR-ghi")
	}
	if !names["nr-lowercase"] {
		t.Error("expected nr-lowercase (prefix is case-insensitive)")
	}
	if names["NR-jkl"] {
		t.Error("NR-jkl should be skipped (suspended)")
	}
	if names["other-vm"] {
		t.Error("other-vm should be skipped (no NR- prefix)")
	}
}

func TestFilterServers_CommissionedWithoutStatus(t *testing.T) {
	servers := []collect.ServerSummary{
		{ID: 1, Name: "NR-built", Commissioned: 3},
		{ID: 2, Name: "NR-installing", Commissioned: 2},
		{ID: 3, Name: "NR-suspended", Commissioned: 3, Suspended: true},
	}
	filtered := collect.FilterServers(servers, "NR-")
	if len(filtered) != 1 || filtered[0].ID != 1 {
		t.Fatalf("expected only commissioned built server, got %+v", filtered)
	}
}

func TestFilterServers_InstallingSkipped(t *testing.T) {
	servers := []collect.ServerSummary{
		{ID: 1, Name: "NR-built", Status: "complete", Commissioned: 3},
		{ID: 2, Name: "NR-installing", Status: "installing"},
	}
	filtered := collect.FilterServers(servers, "NR-")
	if len(filtered) != 1 || filtered[0].ID != 1 {
		t.Fatalf("complete/built should scrape, installing should not: %+v", filtered)
	}
}

func TestHasNamePrefix(t *testing.T) {
	if !collect.HasNamePrefix("NR-abc", "NR-") {
		t.Fatal("NR-abc")
	}
	if !collect.HasNamePrefix("nr-abc", "NR-") {
		t.Fatal("nr-abc should match NR-")
	}
	if collect.HasNamePrefix("other", "NR-") {
		t.Fatal("other should not match")
	}
	if !collect.HasNamePrefix("anything", "") {
		t.Fatal("empty prefix matches all")
	}
}

func TestFilterServers_EmptyPrefix(t *testing.T) {
	servers := []collect.ServerSummary{
		{ID: 1, Name: "any-name", Status: "running"},
		{ID: 2, Name: "stopped-vm", Status: "stopped"},
	}

	filtered := collect.FilterServers(servers, "")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 servers with empty prefix (include stopped), got %d", len(filtered))
	}
}

func TestInventoryCollector_EmitsOnlyNRServerInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/connect":
			_, _ = w.Write([]byte(`{"data":{"version":"7.0"}}`))
		case "/api/v1/servers":
			resp := map[string]any{
				"data": []map[string]any{
					{"id": 1, "name": "NR-abc", "uuid": "u1", "hypervisorId": 11, "commissioned": 3, "suspended": false},
					{"id": 2, "name": "customer-vm", "uuid": "u2", "hypervisorId": 11, "commissioned": 3, "suspended": false},
					{"id": 3, "name": "nr-lower", "uuid": "u3", "hypervisorId": 12, "commissioned": 3, "suspended": 0},
					{"id": 4, "name": "NR-stopped", "uuid": "u4", "hypervisorId": 11, "status": "stopped", "commissioned": 3},
				},
				"meta": map[string]any{"current_page": 1, "last_page": 1},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ic := collect.NewInventoryCollector(vfclient.New(srv.URL, "tok", false), "NR-")
	families, servers, err := ic.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(servers) != 4 {
		t.Fatalf("full list should include non-NR servers, got %d", len(servers))
	}
	if servers[0].HypervisorID != 11 {
		t.Fatalf("expected camelCase hypervisorId parsed, got %d", servers[0].HypervisorID)
	}

	var infoNames []string
	for _, f := range families {
		if f.GetName() != "vf_server_info" {
			continue
		}
		for _, l := range f.Metric[0].Label {
			if l.GetName() == "name" {
				infoNames = append(infoNames, l.GetValue())
			}
		}
	}
	if len(infoNames) != 3 {
		t.Fatalf("vf_server_info should be NR-only, got %v", infoNames)
	}
	got := map[string]bool{}
	for _, n := range infoNames {
		got[n] = true
	}
	if !got["NR-abc"] || !got["nr-lower"] || !got["NR-stopped"] || got["customer-vm"] {
		t.Fatalf("unexpected vf_server_info names: %v", infoNames)
	}

	for _, f := range families {
		if f.GetName() == "vf_guest_up" {
			t.Fatalf("inventory must not emit vf_guest_up (power comes from remoteState), got %s", f.GetName())
		}
	}
}
