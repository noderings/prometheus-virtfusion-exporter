package collect_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/collect"
	"github.com/noderings/prometheus-virtfusion-exporter/internal/vfclient"
)

func TestGuestCollector_GuestUpFromRemoteState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/servers/1":
			_, _ = w.Write([]byte(`{"data":{"id":1,"uuid":"u1","name":"NR-on","remoteState":{"cpu":12,"memory":{"memtotal":"1000","memavailable":"400"}}}}`))
		case "/api/v1/servers/2":
			_, _ = w.Write([]byte(`{"data":{"id":2,"uuid":"u2","name":"NR-off","remoteState":false}}`))
		case "/api/v1/servers/3":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"no"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gc := collect.NewGuestCollector(vfclient.New(srv.URL, "tok", false), 4, 5*time.Second, "NR-")
	servers := []collect.ServerSummary{
		{ID: 1, Name: "NR-on", UUID: "u1", Commissioned: 3},
		{ID: 2, Name: "NR-off", UUID: "u2", Commissioned: 3},
		{ID: 3, Name: "NR-err", UUID: "u3", Commissioned: 3},
		{ID: 4, Name: "customer", UUID: "u4", Commissioned: 3},
	}
	families := gc.Collect(context.Background(), servers)

	guestUp := map[string]float64{}
	for _, f := range families {
		if f.GetName() != "vf_guest_up" {
			continue
		}
		var name string
		for _, l := range f.Metric[0].Label {
			if l.GetName() == "name" {
				name = l.GetValue()
			}
		}
		guestUp[name] = f.Metric[0].GetGauge().GetValue()
	}
	if guestUp["NR-on"] != 1 {
		t.Fatalf("running guest vf_guest_up=1, got %v", guestUp)
	}
	if guestUp["NR-off"] != 0 {
		t.Fatalf("stopped guest vf_guest_up=0, got %v", guestUp)
	}
	if guestUp["NR-err"] != 0 {
		t.Fatalf("fetch error vf_guest_up=0, got %v", guestUp)
	}
	if _, ok := guestUp["customer"]; ok {
		t.Fatalf("non-NR guest should not be scraped: %v", guestUp)
	}
}
