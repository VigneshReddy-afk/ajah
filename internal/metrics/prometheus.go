package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ajah_requests_total",
			Help: "Total LLM requests proxied",
		},
		[]string{"provider", "model", "feature", "status"},
	)

	CostUSDTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ajah_cost_usd_total",
			Help: "Total LLM cost in USD",
		},
		[]string{"feature", "model"},
	)

	LatencyMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ajah_latency_ms",
			Help:    "LLM request latency in milliseconds",
			Buckets: []float64{100, 250, 500, 1000, 2000, 5000, 10000},
		},
		[]string{"provider", "model"},
	)

	HallucinationRisk = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ajah_hallucination_risk",
			Help: "Latest hallucination risk score per feature",
		},
		[]string{"feature"},
	)

	PIIDetectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ajah_pii_detections_total",
			Help: "Total PII detections before storage",
		},
		[]string{"feature"},
	)

	WarningsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ajah_warnings_total",
			Help: "Total warnings fired",
		},
		[]string{"risk_level"},
	)

	ScorerLatencyMs = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ajah_scorer_latency_ms",
			Help:    "Quality scorer latency in milliseconds",
			Buckets: []float64{50, 100, 250, 500, 1000, 2000},
		},
	)

	ClaimDensityRisk = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ajah_claim_density_risk",
			Help: "Latest claim density risk score per feature",
		},
		[]string{"feature"},
	)
)

func Register() {
	prometheus.MustRegister(
		RequestsTotal,
		CostUSDTotal,
		LatencyMs,
		HallucinationRisk,
		PIIDetectionsTotal,
		WarningsTotal,
		ScorerLatencyMs,
		ClaimDensityRisk,
	)
}
