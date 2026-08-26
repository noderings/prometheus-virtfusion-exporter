package http

import (
	"fmt"
	"net/http"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/cache"
	"github.com/prometheus/common/expfmt"
)

// NewMux returns the HTTP handler with /metrics and /healthz.
// /metrics reads only from the in-memory snapshot — never calls VirtFusion.
func NewMux(snap *cache.Snapshot) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metricsHandler(snap))
	mux.HandleFunc("/healthz", healthzHandler())
	return mux
}

func metricsHandler(snap *cache.Snapshot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gatherers := snap.Gatherers()
		mfs, err := gatherers.Gather()
		if err != nil {
			http.Error(w, fmt.Sprintf("gather error: %v", err), http.StatusInternalServerError)
			return
		}

		contentType := expfmt.Negotiate(r.Header)
		w.Header().Set("Content-Type", string(contentType))

		enc := expfmt.NewEncoder(w, contentType)
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				return
			}
		}
	}
}

func healthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}
}
