package cmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	jww "github.com/spf13/jwalterweatherman"
)

type Metrics struct {
	total                  prometheus.Counter
	successful             prometheus.Counter
	failed_empty           prometheus.Counter
	failed_invalid_url     prometheus.Counter // only for /custom endpoint
	failed_unreachable_url prometheus.Counter // only for /custom endpoint
	failed_rpc             prometheus.Counter
	failed_generic         prometheus.Counter // only for /networks endpoint
}

type MetricsKind uint8

const (
	MetricsKindNetworks MetricsKind = iota
	MetricsKindGeneric
	MetricsKindCustom
)

func NewMetrics(uri string, kind MetricsKind) *Metrics {
	mod_uri := strings.ReplaceAll(uri, "/", "_")
	// All kinds have total and successful
	metrics := &Metrics{
		total: promauto.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("requests%s_total", mod_uri),
			Help: fmt.Sprintf("Total number of requests for %s", uri),
		}),
		successful: promauto.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("requests%s_successful", mod_uri),
			Help: fmt.Sprintf("Total number of successful requests for %s", uri),
		}),
	}
	// Only /networks has failed_generic
	if kind == MetricsKindNetworks {
		metrics.failed_generic = promauto.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("requests%s_failed_generic", mod_uri),
			Help: fmt.Sprintf("Total number of failed requests for %s due to generic error", uri),
		})
	} else {
		// Both generic and custom have failed_empty and failed_rpc
		metrics.failed_empty = promauto.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("requests%s_failed_empty", mod_uri),
			Help: fmt.Sprintf("Total number of failed requests for %s due to empty response", uri),
		})
		metrics.failed_rpc = promauto.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("requests%s_failed_rpc", mod_uri),
			Help: fmt.Sprintf("Total number of failed requests for %s due to RPC error", uri),
		})
	}
	// Only /custom has failed_invalid_url and failed_unreachable_url
	if kind == MetricsKindCustom {
		metrics.failed_invalid_url = promauto.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("requests%s_failed_invalid_url", mod_uri),
			Help: fmt.Sprintf("Total number of failed requests for %s due to invalid URL", uri),
		})
		metrics.failed_unreachable_url = promauto.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("requests%s_failed_unreachable_url", mod_uri),
			Help: fmt.Sprintf("Total number of failed requests for %s due to unreachable URL", uri),
		})
	}
	return metrics
}

func (m *Metrics) IncTotal() {
	m.total.Inc()
}

func (m *Metrics) IncSuccessful() {
	m.successful.Inc()
}

func (m *Metrics) IncFailedEmpty() {
	m.failed_empty.Inc()
}

func (m *Metrics) IncFailedInvalidUrl() {
	m.failed_invalid_url.Inc()
}

func (m *Metrics) IncFailedUnreachableUrl() {
	m.failed_unreachable_url.Inc()
}

func (m *Metrics) IncFailedRpc() {
	m.failed_rpc.Inc()
}

func (m *Metrics) IncFailedGeneric() {
	m.failed_generic.Inc()
}

type MetricsServer struct {
	port int
	srv  *http.Server
}

func NewMetricsServer(port int) *MetricsServer {
	ms := &MetricsServer{port, nil}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	// Expose the registered metrics via HTTP server
	ms.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	return ms
}

func (s *MetricsServer) Start() {
	jww.INFO.Printf("[%s] Starting metrics HTTP server on port %d", logPrefix, s.port)
	if err := s.srv.ListenAndServe(); err != http.ErrServerClosed {
		jww.FATAL.Panicf("[%s] Error starting metrics HTTP server", logPrefix)
	}
}

func (s *MetricsServer) Stop() {
	jww.INFO.Printf("[%s] Stopping metrics HTTP server on port %d", logPrefix, s.port)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer func() {
		cancel()
	}()
	if err := s.srv.Shutdown(ctx); err != nil {
		jww.FATAL.Panicf("[%s] Error stopping metrics HTTP server: %v", logPrefix, err)
	}
	jww.INFO.Printf("[%s] Metrics HTTP server stopped", logPrefix)
}
