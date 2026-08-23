// PriceImpactMetric measures price impact as a function of trade size — how
// much worse the effective rate gets as the trade grows.
//
// A corridor with a flat curve and a bad floor is a different problem from
// one with a good floor and a cliff. The first is priced badly, the second
// is thin. Separating the two tells a user whether their size is the problem
// or the corridor is.
//
// This reports a single price impact figure: the degradation between the
// smallest probe size and the largest requested size. A curve-shaped metric
// is future work once the ladder infrastructure supports it.
//
// This is a metric, not a check. No threshold exists yet.
package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
)

// PriceImpactMetric measures how much the effective rate degrades with size.
type PriceImpactMetric struct {
	// DEX is the Horizon client used to price paths.
	DEX *dex.Client

	// ProbeSize is the small trade size used as the baseline for comparison.
	// Defaults to 1 unit if zero.
	ProbeSize decimal.Decimal

	// FullSize is the large trade size to compare against. Defaults to
	// the largest of route.DefaultSizes if zero.
	FullSize decimal.Decimal
}

// Describe implements Metric.
func (PriceImpactMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "price-impact.size",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		Title: "Price impact as a function of trade size",
		CanDetermine: "How much the effective rate degrades between a small " +
			"probe and a full-size trade, as a percentage. Measured via " +
			"Horizon pathfinding at two sizes.",
		CannotDetermine: "The full curve shape — this reports the single " +
			"degradation figure between probe and full size. The ladder's " +
			"individual rungs carry the curve data separately.",
	}
}

// Run implements Metric.
//
// It prices the corridor at a small probe size and a full size, and reports
// the degradation between them as a percentage. Fewer than two priced sizes
// or no market at either size makes the result undetermined.
func (m PriceImpactMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}

	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available to price paths")
	}

	probe := m.ProbeSize
	if probe.IsZero() {
		probe = decimal.NewFromInt(1)
	}

	full := m.FullSize
	if full.IsZero() {
		full = decimal.NewFromInt(5000)
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/paths/strict-send %s/%s", s.Send.Code, s.Receive.Code),
		ObservedAt: at,
	}

	// Price at both sizes. If either fails, the metric is undetermined.
	probePath, probeErr := m.DEX.BestPath(ctx, s.Send, probe, s.Receive)
	fullPath, fullErr := m.DEX.BestPath(ctx, s.Send, full, s.Receive)

	switch {
	case probeErr != nil && fullPath != nil:
		// Probe failed but full succeeded — we have one point, not a curve.
		evidence.Observed = fmt.Sprintf("probe=%s: error: %v, full=%s: %s",
			probe, probeErr, full, fullPath.DestAmount)
		return MetricUndetermined(d, s,
			fmt.Sprintf("probe size %s failed: %v", probe, probeErr), evidence)

	case probeErr != nil && fullPath == nil:
		evidence.Observed = fmt.Sprintf("probe=%s: error: %v, full=%s: error: %v",
			probe, probeErr, full, fullErr)
		return MetricUndetermined(d, s,
			fmt.Sprintf("both sizes failed: probe=%v, full=%v", probeErr, fullErr), evidence)

	case probePath == nil:
		return MetricUndetermined(d, s, "no path found at probe size", evidence)

	case fullPath == nil:
		evidence.Observed = fmt.Sprintf("probe=%s: %s, full=%s: no path",
			probe, probePath.DestAmount, full)
		return MetricUndetermined(d, s, "no path found at full size", evidence)
	}

	probeRate := probePath.Rate()
	fullRate := fullPath.Rate()

	if probeRate.IsZero() {
		evidence.Observed = fmt.Sprintf("probe=%s: rate=0, full=%s: %s",
			probe, full, fullPath.DestAmount)
		return MetricUndetermined(d, s, "probe rate is zero, cannot compute impact", evidence)
	}

	// Impact = (probe_rate - full_rate) / probe_rate * 100
	// Positive means the larger trade gets a worse rate.
	impact := probeRate.Sub(fullRate).Div(probeRate).Mul(decimal.NewFromInt(100))

	// Negative impact means the larger trade got a better rate, which is
	// theoretically possible but practically signals a measurement artifact.
	// Report as zero rather than a misleading negative.
	if impact.IsNegative() {
		impact = decimal.Zero
	}

	evidence.Observed = fmt.Sprintf(
		"probe=%s: dest=%s, rate=%s; full=%s: dest=%s, rate=%s; impact=%s%%",
		probe, probePath.DestAmount, probeRate.StringFixed(4),
		full, fullPath.DestAmount, fullRate.StringFixed(4),
		impact.StringFixed(4))

	summary := fmt.Sprintf(
		"price impact %s%% from %s to %s %s",
		impact.StringFixed(2), probe, full, s.Send.Code)

	return MetricValue(d, s, impact, UnitPercent, summary, evidence)
}
