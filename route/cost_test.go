package route

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

func testUSDC() asset.Asset { return asset.USDC() }
func testNGNC() asset.Asset { return asset.NGNC() }

// TestCostDecomposeSplitsCorrectly checks the decomposition produces all four
// components with the expected structure.
func TestCostDecomposeSplitsCorrectly(t *testing.T) {
	// A quote with 25% loss — realistic for the USDC/NGNC corridor at
	// a small size.
	q := Quote{
		Kind:          KindDEX,
		Description:   "USDC -> XLM -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("112800.51"),
		EffectiveRate: decimal.RequireFromString("1128.0051"),
		ReferenceMid:  decimal.RequireFromString("1500"),
		LossPct:       decimal.RequireFromString("24.80"),
		LossAmount:    decimal.RequireFromString("37199.49"),
		Verdict:       VerdictUnusable,
	}

	mid := decimal.RequireFromString("1500")
	d := Decompose(q, mid)

	if d.TotalLossPct.StringFixed(2) != "24.80" {
		t.Errorf("TotalLossPct = %s, want 24.80", d.TotalLossPct)
	}

	if len(d.Parts) != 4 {
		t.Fatalf("expected 4 cost parts, got %d", len(d.Parts))
	}

	// Verify all four components are present
	seen := map[CostComponent]bool{}
	for _, p := range d.Parts {
		seen[p.Component] = true
	}
	for _, comp := range []CostComponent{CostFXLoss, CostFees, CostSlippage, CostExpectedFailure} {
		if !seen[comp] {
			t.Errorf("missing cost component: %s", comp)
		}
	}

	// FX loss should equal the total loss for a DEX route
	fxLoss := d.Parts[0]
	if fxLoss.Component != CostFXLoss {
		t.Errorf("first component = %s, want fx_loss", fxLoss.Component)
	}
	if !fxLoss.Determined {
		t.Error("FX loss should be determined")
	}
	if fxLoss.Pct.StringFixed(2) != "24.80" {
		t.Errorf("FX loss pct = %s, want 24.80", fxLoss.Pct)
	}

	// Fees should be zero for a DEX route (negligible network fees)
	fees := d.Parts[1]
	if fees.Component != CostFees {
		t.Errorf("second component = %s, want fees", fees.Component)
	}
	if !fees.Determined {
		t.Error("fees should be determined (negligible for DEX)")
	}

	// Slippage should be undetermined (needs comparison across sizes)
	slippage := d.Parts[2]
	if slippage.Component != CostSlippage {
		t.Errorf("third component = %s, want slippage", slippage.Component)
	}
	if slippage.Determined {
		t.Error("slippage should be undetermined without a size comparison")
	}
	if slippage.Reason == "" {
		t.Error("undetermined slippage must carry a reason")
	}

	// Expected failure cost must always be undetermined
	failCost := d.Parts[3]
	if failCost.Component != CostExpectedFailure {
		t.Errorf("fourth component = %s, want expected_failure", failCost.Component)
	}
	if failCost.Determined {
		t.Error("expected failure cost must be undetermined — no history exists")
	}
	if failCost.Reason == "" {
		t.Error("undetermined expected failure cost must carry a reason")
	}
}

// TestCostDecomposeZeroLoss covers the edge case of a route at mid (no loss).
func TestCostDecomposeZeroLoss(t *testing.T) {
	q := Quote{
		Kind:        KindDEX,
		SendAsset:   testUSDC(),
		SendAmount:  decimal.NewFromInt(100),
		ReceiveAsset: testNGNC(),
		ReceiveAmount: decimal.NewFromInt(150000),
		EffectiveRate: decimal.NewFromInt(1500),
		ReferenceMid:  decimal.NewFromInt(1500),
		LossPct:       decimal.Zero,
		LossAmount:    decimal.Zero,
		Verdict:       VerdictGood,
	}

	d := Decompose(q, decimal.NewFromInt(1500))

	if !d.TotalLossPct.IsZero() {
		t.Errorf("TotalLossPct = %s, want zero", d.TotalLossPct)
	}

	fxLoss := d.Parts[0]
	if !fxLoss.Pct.IsZero() {
		t.Errorf("FX loss pct = %s, want zero at mid", fxLoss.Pct)
	}
}

// TestCostComponentsDoNotOverlap verifies that FX loss equals the total loss
// for a DEX route, and that the other determined components do not double-count.
func TestCostComponentsDoNotOverlap(t *testing.T) {
	q := Quote{
		Kind:        KindDEX,
		SendAsset:   testUSDC(),
		SendAmount:  decimal.NewFromInt(100),
		ReceiveAsset: testNGNC(),
		ReceiveAmount: decimal.RequireFromString("75100"),
		EffectiveRate: decimal.RequireFromString("751"),
		ReferenceMid:  decimal.NewFromInt(1000),
		LossPct:       decimal.RequireFromString("24.9"),
		LossAmount:    decimal.RequireFromString("24900"),
		Verdict:       VerdictPoor,
	}

	d := Decompose(q, decimal.NewFromInt(1000))

	// Sum the determined components
	sumPct := decimal.Zero
	for _, p := range d.Parts {
		if p.Determined {
			sumPct = sumPct.Add(p.Pct)
		}
	}

	// For a DEX route with only FX loss determined, the sum should equal
	// the total loss.
	if sumPct.StringFixed(2) != d.TotalLossPct.StringFixed(2) {
		t.Errorf("sum of determined components = %s, total loss = %s — they should match for a DEX route",
			sumPct.StringFixed(2), d.TotalLossPct.StringFixed(2))
	}
}

// TestCostDecomposeReasonsAreNonEmpty verifies every undetermined component
// carries a reason, as the contract requires.
func TestCostDecomposeReasonsAreNonEmpty(t *testing.T) {
	q := Quote{
		Kind:        KindDEX,
		SendAsset:   testUSDC(),
		SendAmount:  decimal.NewFromInt(100),
		ReceiveAsset: testNGNC(),
		LossPct:     decimal.RequireFromString("50"),
	}

	d := Decompose(q, decimal.NewFromInt(1500))

	for _, p := range d.Parts {
		if !p.Determined && strings.TrimSpace(p.Reason) == "" {
			t.Errorf("component %s is undetermined but has no reason", p.Component)
		}
	}
}
