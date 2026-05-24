package queuedispatcher

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics is the set of Prometheus collectors the dispatcher emits.
// Spec: TASKS_COMPUTE_PIPELINES.md Task A3 acceptance criteria —
// pipeline_build_queue_depth{pool}, pipeline_build_wait_seconds{
// pool,priority}, pipeline_build_pool_utilization{pool}.
//
// All three labels match the spec verbatim so existing Grafana
// dashboards / alert rules referencing them keep working.
type Metrics struct {
	QueueDepth      *prometheus.GaugeVec
	PoolUtilization *prometheus.GaugeVec
	WaitSeconds     *prometheus.HistogramVec
	TickPromoted    prometheus.Counter
	TickWaiting     prometheus.Counter
	TickErrors      prometheus.Counter
}

// NewMetrics constructs the collectors. The caller is responsible
// for registering them with their service's Prometheus registry —
// returned unregistered so tests can stand them up in isolation.
func NewMetrics() *Metrics {
	return &Metrics{
		QueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pipeline_build_queue_depth",
			Help: "Pipeline runs currently in QUEUED state, labeled by target resource pool.",
		}, []string{"pool"}),

		PoolUtilization: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pipeline_build_pool_utilization",
			Help: "Per-pool utilization as the ratio of running builds to max_concurrent_builds. Unbounded pools report 0 (no cap to normalize against).",
		}, []string{"pool"}),

		WaitSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pipeline_build_wait_seconds",
			Help:    "Wall-clock seconds a build sat in QUEUED before being promoted to RUNNING by the dispatcher.",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 12), // 0.5s … ~17min
		}, []string{"pool", "priority"}),

		TickPromoted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pipeline_build_dispatcher_promoted_total",
			Help: "Cumulative count of queued runs the dispatcher has promoted to RUNNING.",
		}),
		TickWaiting: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pipeline_build_dispatcher_waiting_total",
			Help: "Cumulative count of queued runs the dispatcher left in QUEUED because no pool had capacity this tick.",
		}),
		TickErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pipeline_build_dispatcher_errors_total",
			Help: "Cumulative count of per-run failures inside a dispatcher Tick (utilization lookup, reserve, promote).",
		}),
	}
}

// Collectors returns every collector in registration order, useful
// for one-shot Metrics.Register(...) wiring.
func (m *Metrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{
		m.QueueDepth, m.PoolUtilization, m.WaitSeconds,
		m.TickPromoted, m.TickWaiting, m.TickErrors,
	}
}
