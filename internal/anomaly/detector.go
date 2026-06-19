package anomaly

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/ajah/core/internal/storage"
	"go.uber.org/zap"
)

const zScoreThreshold = 2.5

// AnomalyType identifies the kind of statistical anomaly detected.
type AnomalyType string

const (
	LatencySpike       AnomalyType = "latency_spike"
	CostSpike          AnomalyType = "cost_spike"
	HallucinationSurge AnomalyType = "hallucination_surge"
	QualityDrop        AnomalyType = "quality_drop"
)

// Anomaly is a single detected deviation from a feature's baseline.
type Anomaly struct {
	RequestID   string
	FeatureName string
	Type        AnomalyType
	Value       float64
	Baseline    float64
	ZScore      float64
	DetectedAt  time.Time
}

// Detector holds per-feature baselines in memory and evaluates each request
// against them. Baselines are refreshed periodically by the caller.
type Detector struct {
	mu        sync.RWMutex
	baselines map[string]storage.FeatureBaseline
	writer    *storage.Writer
	logger    *zap.Logger
}

// New creates a Detector. Call RefreshBaselines before the first Detect call.
func New(writer *storage.Writer, logger *zap.Logger) *Detector {
	return &Detector{
		baselines: make(map[string]storage.FeatureBaseline),
		writer:    writer,
		logger:    logger,
	}
}

// RefreshBaselines pulls the last 7 days of per-feature statistics from
// ClickHouse and replaces the in-memory baseline map atomically.
func (d *Detector) RefreshBaselines(ctx context.Context) error {
	baselines, err := d.writer.QueryFeatureBaselines(ctx, 7)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baselines = make(map[string]storage.FeatureBaseline, len(baselines))
	for _, b := range baselines {
		d.baselines[b.FeatureName] = b
	}
	d.logger.Info("anomaly baselines refreshed", zap.Int("features", len(baselines)))
	return nil
}

func zScore(value, mean, std float64) float64 {
	if std < 0.0001 {
		return 0
	}
	return math.Abs(value-mean) / std
}

// Detect compares the supplied per-request metrics against the stored baseline
// for featureName and returns any anomalies whose z-score exceeds the
// threshold. Returns nil if no baseline exists yet for this feature.
func (d *Detector) Detect(
	requestID string,
	featureName string,
	hallucinationRisk float64,
	costUSD float64,
	latencyMs int64,
	qualityScore float64,
) []Anomaly {
	d.mu.RLock()
	baseline, ok := d.baselines[featureName]
	d.mu.RUnlock()

	if !ok || baseline.SampleCount < 10 {
		return nil
	}

	var anomalies []Anomaly
	now := time.Now()

	if z := zScore(hallucinationRisk, baseline.AvgHallucination, baseline.StdHallucination); z >= zScoreThreshold {
		anomalies = append(anomalies, Anomaly{
			RequestID:   requestID,
			FeatureName: featureName,
			Type:        HallucinationSurge,
			Value:       hallucinationRisk,
			Baseline:    baseline.AvgHallucination,
			ZScore:      z,
			DetectedAt:  now,
		})
	}

	if z := zScore(costUSD, baseline.AvgCostUSD, baseline.StdCostUSD); z >= zScoreThreshold {
		anomalies = append(anomalies, Anomaly{
			RequestID:   requestID,
			FeatureName: featureName,
			Type:        CostSpike,
			Value:       costUSD,
			Baseline:    baseline.AvgCostUSD,
			ZScore:      z,
			DetectedAt:  now,
		})
	}

	if z := zScore(float64(latencyMs), baseline.AvgLatencyMs, baseline.StdLatencyMs); z >= zScoreThreshold {
		anomalies = append(anomalies, Anomaly{
			RequestID:   requestID,
			FeatureName: featureName,
			Type:        LatencySpike,
			Value:       float64(latencyMs),
			Baseline:    baseline.AvgLatencyMs,
			ZScore:      z,
			DetectedAt:  now,
		})
	}

	// Quality drop is inverted: low score is the anomaly.
	if qualityScore > 0 {
		if z := zScore(qualityScore, baseline.AvgQualityScore, baseline.StdQualityScore); z >= zScoreThreshold &&
			qualityScore < baseline.AvgQualityScore {
			anomalies = append(anomalies, Anomaly{
				RequestID:   requestID,
				FeatureName: featureName,
				Type:        QualityDrop,
				Value:       qualityScore,
				Baseline:    baseline.AvgQualityScore,
				ZScore:      z,
				DetectedAt:  now,
			})
		}
	}

	return anomalies
}
