// File: controllers/metrics.go
package controllers

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	reconcileCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tekton_controller",
			Name:      "http_proxy_reconcile_total",
			Help:      "Number of times HandleHTTPProxyListener has been invoked, by result.",
		},
		[]string{"result"},
	)
	reconcileDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "tekton_controller",
			Name:      "http_proxy_reconcile_duration_seconds",
			Help:      "Duration of HandleHTTPProxyListener calls.",
			Buckets:   prometheus.DefBuckets,
		},
	)
)

func init() {
	prometheus.MustRegister(reconcileCounter, reconcileDuration)
}

// observe wraps a function f, recording metrics automatically.
func observe(f func() error) (err error) {
	start := time.Now()
	defer func() {
		d := time.Since(start).Seconds()
		reconcileDuration.Observe(d)
		if err != nil {
			reconcileCounter.WithLabelValues("error").Inc()
		} else {
			reconcileCounter.WithLabelValues("success").Inc()
		}
	}()
	err = f()
	return
}
