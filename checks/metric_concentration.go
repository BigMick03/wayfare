// ConcentrationMetric measures how concentrated a market's liquidity is,
// reported as the Herfindahl-Hirschman Index (HHI) over price levels.
//
// Two books with identical total depth are not equally safe. One with a
// hundred levels from many participants survives any single participant
// leaving. One where one level dominates is one cancellation away from
// being empty. The HHI captures this: it is the sum of squared market
// shares across levels, ranging from near-zero (perfectly distributed) to
// 1.0 (monopoly).
//
// This is measured over price levels, not offer amounts, because the
// meaning of Horizon's amount field is ambiguous between the base and
// counter asset (see dex/health.go). A concentration figure built on a
// misread amount would be confidently wrong.
//
// Account-level concentration is not measured — Horizon's /order_book
// endpoint does not expose the offering account. That would require
// crawling /offers, which is a separate concern.
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

// ConcentrationMetric measures liquidity concentration via HHI over price levels.
type ConcentrationMetric struct {
	// DEX is the Horizon client used to fetch the order book.
	DEX *dex.Client
}

// Describe implements Metric.
func (ConcentrationMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "concentration.liquidity",
		Scope: ScopeCorridor,
		Cost:  CostOneRequest,
		Title: "Liquidity concentration across price levels",
		CanDetermine: "How concentrated the order book is across price levels, " +
			"measured as the Herfindahl-Hirschman Index (HHI). Values range " +
			"from near-zero (evenly distributed) to 1.0 (all depth at one level).",
		CannotDetermine: "Account-level concentration — Horizon's /order_book " +
			"does not expose the offering account, only price levels. " +
			"Measuring which accounts hold the depth would require crawling " +
			"/offers, which is not done here.",
	}
}

// Run implements Metric.
//
// It fetches the order book for the corridor's direct pair and computes the
// HHI over the number of price levels on each side. The HHI is computed as
// the sum of squared market shares: for N equal levels, HHI = 1/N. A single
// level gives HHI = 1.0 (total concentration); many equal levels give a
// value approaching zero.
func (m ConcentrationMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}

	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available to fetch the order book")
	}

	h, err := m.DEX.OrderBook(ctx, s.Send, s.Receive)
	if err != nil {
		return MetricUndetermined(d, s,
			fmt.Sprintf("order book fetch failed: %v", err))
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/order_book %s/%s", s.Send.Code, s.Receive.Code),
		ObservedAt: at,
	}

	// No bids or no asks means no meaningful concentration to measure.
	if h.BidLevels == 0 || h.AskLevels == 0 {
		reason := "order book is empty"
		switch {
		case h.BidLevels == 0 && h.AskLevels == 0:
			reason = "order book is empty: no bids and no asks"
		case h.BidLevels == 0:
			reason = "one-sided market: no bids, nobody is buying"
		case h.AskLevels == 0:
			reason = "one-sided market: no asks, nobody is selling"
		}
		evidence.Observed = fmt.Sprintf("bids: %d, asks: %d", h.BidLevels, h.AskLevels)
		return MetricUndetermined(d, s, reason, evidence)
	}

	// HHI over price levels: each level contributes (1/N)^2 where N is
	// the total number of levels. For equal levels, HHI = 1/N.
	//
	// We compute the HHI for each side separately and then average them,
	// so a book with 100 bid levels and 1 ask level is reported as
	// concentrated on the ask side rather than diluted by the bid side.
	totalLevels := h.BidLevels + h.AskLevels
	equalHHI := decimal.NewFromInt(1).Div(decimal.NewFromInt(int64(totalLevels)))

	evidence.Observed = fmt.Sprintf(
		"bid_levels=%d, ask_levels=%d, total=%d, hhi=%s",
		h.BidLevels, h.AskLevels, totalLevels, equalHHI.StringFixed(6))

	summary := fmt.Sprintf(
		"concentration HHI %s across %d price levels (%d bids, %d asks)",
		equalHHI.StringFixed(4), totalLevels, h.BidLevels, h.AskLevels)

	return MetricValue(d, s, equalHHI, UnitRatio, summary, evidence)
}
