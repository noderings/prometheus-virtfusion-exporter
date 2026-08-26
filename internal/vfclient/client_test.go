package vfclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/vfclient"
)

func TestConnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/connect" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong auth header: %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"version":"7.0"}}`))
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "test-token", false)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
}

func TestConnect_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthenticated."}`))
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "bad-token", false)
	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestListServers_Pagination(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/servers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		page++
		switch page {
		case 1:
			resp := map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": 1, "name": "NR-abc", "uuid": "uuid-1", "hypervisor_id": 10, "status": "running"},
				},
				"meta": map[string]interface{}{"current_page": 1, "last_page": 2},
			}
			json.NewEncoder(w).Encode(resp)
		case 2:
			resp := map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": 2, "name": "NR-def", "uuid": "uuid-2", "hypervisor_id": 10, "status": "running"},
				},
				"meta": map[string]interface{}{"current_page": 2, "last_page": 2},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "tok", false)
	servers, err := c.ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
}

func TestGetServerRemoteState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/servers/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("remoteState") != "true" {
			t.Error("expected remoteState=true")
		}
		resp := `{"data":{"id":42,"uuid":"u-42","name":"NR-test","remoteState":{"cpuUsage":55.2}}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "tok", false)
	raw, err := c.GetServerRemoteState(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetServerRemoteState: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data["id"].(float64) != 42 {
		t.Errorf("expected id 42, got %v", data["id"])
	}
}

func TestListHypervisors_NestedGroupID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/compute/hypervisors" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := `{"data":[{"id":1,"name":"hv1","enabled":true,"group":{"id":10}}]}`
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "tok", false)
	hvs, err := c.ListHypervisors(context.Background())
	if err != nil {
		t.Fatalf("ListHypervisors: %v", err)
	}
	if len(hvs) != 1 || hvs[0].ResolvedGroupID() != 10 {
		t.Fatalf("expected nested group.id=10, got %+v", hvs)
	}
}

func TestGetGroupResources_PathAndArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/compute/hypervisors/groups/10/resources" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := `{
			"data": [
				{
					"hypervisor": {"id": 11, "name": "hv1", "enabled": true},
					"resources": {
						"cpuCores": {"allocated": 8, "max": 32},
						"memory": {"allocated": 4096, "max": 16384, "units": "MB"},
						"localStorage": {"allocated": 100, "max": 500, "units": "GB"},
						"otherStorage": [{"allocated": 20, "max": 80, "units": "GB"}]
					}
				}
			],
			"meta": {"current_page": 1, "last_page": 1}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "tok", false)
	rows, err := c.GetGroupResources(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetGroupResources: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Hypervisor.ID != 11 {
		t.Fatalf("expected hypervisor 11, got %d", rows[0].Hypervisor.ID)
	}
	if rows[0].Resources.CPUCores.Allocated != 8 {
		t.Fatalf("cpu allocated: %v", rows[0].Resources.CPUCores.Allocated)
	}
	if rows[0].Resources.DiskAllocated() != 120 {
		t.Fatalf("disk allocated local+other: %v", rows[0].Resources.DiskAllocated())
	}
}

func TestGetServerTraffic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"data":{"monthly":[{"rx":1000,"tx":2000,"total":3000,"limit":100}]}}`
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "tok", false)
	tr, err := c.GetServerTraffic(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetServerTraffic: %v", err)
	}
	if tr.Total != 3000 {
		t.Errorf("expected total 3000, got %v", tr.Total)
	}
	if tr.Limit != 100 {
		t.Errorf("expected limit 100, got %v", tr.Limit)
	}
}

func TestBearerAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-secret-token" {
			t.Errorf("expected Bearer my-secret-token, got %s", auth)
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := vfclient.New(srv.URL, "my-secret-token", false)
	_ = c.Connect(context.Background())
}
