package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewServer builds the :port/metrics HTTP server. It mirrors how the gRPC
// server is built in main.go -- constructed here, started in a goroutine and
// shut down by the caller, not self-managed.
func NewServer(port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
}
