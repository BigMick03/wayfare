// Metric measures a quantity about a subject, producing a MetricResult rather
// than a boolean CheckResult.
//
// Metrics and checks share the same Observation infrastructure but answer
// different questions. A check asks "does this fact hold?" and answers
// passed/failed/undetermined. A metric asks "what is this quantity?" and
// answers a decimal value with a unit. A spread of 2% and a spread of 200%
// are both valid metric results; collapsing them into "pass" and "fail"
// discards the number that carries the meaning.
package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Metric is one measured quantity about a subject.
//
// Implementations must never panic, never block indefinitely, and never return
// a determined result they cannot evidence. Run receives a context and is
// expected to honour it.
type Metric interface {
	Describe() Descriptor
	Run(ctx context.Context, s Subject) MetricResult
}

// RunMetric executes one metric, converting a panic into an undetermined result.
//
// A contributed metric that panics must not take down a sweep. The other
// corridors are still worth measuring, and a metric that crashed is exactly
// the kind of thing a reader should be told about rather than shielded from.
func RunMetric(ctx context.Context, m Metric, s Subject) (res MetricResult) {
	d := m.Describe()

	defer func() {
		if r := recover(); r != nil {
			res = metricUndetermined(d, s, fmt.Sprintf("metric panicked: %v", r))
		}
	}()

	if err := d.Validate(); err != nil {
		return metricUndetermined(d, s, err.Error())
	}
	if err := ctx.Err(); err != nil {
		return metricUndetermined(d, s, "context cancelled before the metric ran: "+err.Error())
	}

	res = m.Run(ctx, s)

	// Repair missing identity rather than discard. The measurement still
	// happened even if the implementation forgot to fill in boilerplate.
	if res.ID == "" {
		res.ID = d.ID
	}
	if res.Subject == "" {
		res.Subject = s.Label()
	}
	if res.At.IsZero() {
		res.At = time.Now().UTC()
	}
	if !res.Determined && strings.TrimSpace(res.Reason) == "" {
		res.Reason = "no reason given — this is a bug in metric " + d.ID
	}
	return res
}

// MetricUndetermined builds a metric result for a quantity that could not be
// measured. The reason is mandatory at the call site because it is mandatory
// in the contract: a result that says only "unknown" tells a reader nothing
// about whether to look further.
func MetricUndetermined(d Descriptor, s Subject, reason string, ev ...Evidence) MetricResult {
	return metricUndetermined(d, s, reason, ev...)
}

func metricUndetermined(d Descriptor, s Subject, reason string, ev ...Evidence) MetricResult {
	if strings.TrimSpace(reason) == "" {
		reason = "no reason given — this is a bug in metric " + d.ID
	}
	return MetricResult{
		Observation: Observation{
			ID: d.ID, Scope: d.Scope, Subject: s.Label(),
			At: time.Now().UTC(), Determined: false, Reason: reason, Evidence: ev,
		},
		Summary: "could not determine: " + reason,
	}
}

// MetricValue builds a determined metric result with a decimal value.
func MetricValue(d Descriptor, s Subject, value decimal.Decimal, unit Unit, summary string, ev ...Evidence) MetricResult {
	return MetricResult{
		Observation: Observation{
			ID: d.ID, Scope: d.Scope, Subject: s.Label(),
			At: time.Now().UTC(), Determined: true, Evidence: ev,
		},
		Value:   value,
		Unit:    unit,
		Summary: summary,
	}
}

// RunMetricAll executes every metric against one subject, in declaration order.
func RunMetricAll(ctx context.Context, ms []Metric, s Subject) []MetricResult {
	out := make([]MetricResult, 0, len(ms))
	for _, m := range ms {
		out = append(out, RunMetric(ctx, m, s))
	}
	return out
}
