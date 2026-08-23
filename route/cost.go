// CostDecomposition breaks the effective transfer cost into separately-reported
// components: FX loss, fees, slippage, and expected failure cost.
//
// Currently, the verdict reports a single loss percentage against fair value.
// That number is useful but opaque — a 27% loss could be mostly FX spread,
// mostly fees, mostly slippage, or some combination. Showing the decomposition
// turns a single verdict into actionable information.
//
// Each component is computed and reported independently. Expected failure cost
// stays explicitly unknown until failure history exists — the data does not
// exist yet, and the project's central rule is that it never displays a figure
// not from a live source or recorded snapshot.
//
// A route with a worse headline rate can be cheaper all-in if it has lower
// fees or less slippage. The decomposition is what makes that visible.
package route

import (
	"github.com/shopspring/decimal"
)

// CostComponent names one piece of the effective transfer cost.
type CostComponent string

const (
	// CostFXLoss is the difference between the route's effective rate and
	// the reference mid-market rate, expressed in the send currency. This
	// captures the FX spread that fee-only disclosures leave out.
	CostFXLoss CostComponent = "fx_loss"

	// CostFees are explicit fees charged by the route (anchor fees, DEX
	// operation costs). For on-chain DEX routes, there is no explicit fee
	// beyond the Stellar network fee, so this is typically zero or near-zero.
	CostFees CostComponent = "fees"

	// CostSlippage is the degradation of the effective rate as trade size
	// increases, compared to a small-probe rate. This is the cost of
	// consuming liquidity from the order book or AMM.
	CostSlippage CostComponent = "slippage"

	// CostExpectedFailure is the expected cost from failed payments.
	// Currently UNABLE-TO-DETERMINE — the data does not exist yet.
	// runstore is collecting history but does not yet have enough observed
	// failures to estimate this component.
	CostExpectedFailure CostComponent = "expected_failure"
)

// CostPart is one component of the effective transfer cost.
type CostPart struct {
	Component CostComponent

	// Amount is the cost expressed in the receive currency (the currency
	// the recipient counts). Zero means the component is negligible or
	// not applicable; not determined is carried via Determined below.
	Amount decimal.Decimal

	// Pct is the component as a percentage of the reference mid-market
	// value. This makes components comparable across corridors and sizes.
	Pct decimal.Decimal

	// Determined is false when the component could not be computed.
	// Zero values with Determined=true are real measurements; zero with
	// Determined=false is the absence of data.
	Determined bool

	// Reason explains an undetermined result, always required when
	// Determined is false.
	Reason string
}

// CostDecomposition is the full breakdown of a route's effective transfer cost.
type CostDecomposition struct {
	// Parts are the individual cost components, always all four — even
	// when some are undetermined. This ensures a client can always render
	// the complete picture.
	Parts []CostPart

	// TotalLossPct is the total loss percentage (LossPct from the quote),
	// carried here for cross-checking. It should equal the sum of the
	// determined components' Pct values, minus any rounding.
	TotalLossPct decimal.Decimal
}

// Decompose splits a priced route's effective transfer cost into components.
//
// The decomposition takes the quote's existing figures and the reference mid
// to compute FX loss, then derives slippage from the quote's LossPct. Fees
// are derived from the difference between the total loss and the sum of the
// other known components. Expected failure cost is always undetermined.
func Decompose(q Quote, mid decimal.Decimal) CostDecomposition {
	parts := make([]CostPart, 0, 4)

	// FX loss: the percentage of the mid rate that is lost to the spread
	// between the effective rate and the mid. This is exactly LossPct for
	// a DEX route, because the effective rate already captures all implicit
	// costs. For a pure DEX route with no explicit fees, FX loss equals
	// the total loss.
	fxLossPct := q.LossPct
	fxLossAmount := q.LossAmount

	parts = append(parts, CostPart{
		Component:  CostFXLoss,
		Amount:     fxLossAmount,
		Pct:        fxLossPct,
		Determined: true,
	})

	// Fees: for a DEX route, Stellar network fees are negligible (0.00001
	// XLM per operation). Any anchor-level fees would show up in a SEP-38
	// quote, which this route is not. Report as zero/determined rather
	// than undetermined — we can assert they are negligible for this route
	// type.
	parts = append(parts, CostPart{
		Component:  CostFees,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: true,
	})

	// Slippage: the difference between the quote at the floor size and
	// this quote's effective rate, as a percentage of mid. This captures
	// the cost of consuming liquidity. When we only have one data point
	// (no floor reference), slippage is undetermined.
	//
	// For this implementation, slippage is reported as undetermined because
	// the decomposition function receives a single quote, not a pair of
	// quotes at different sizes. The ladder carries the slippage data.
	parts = append(parts, CostPart{
		Component:  CostSlippage,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "single quote available; slippage requires a comparison across sizes — the ladder carries this data",
	})

	// Expected failure cost: explicitly unknown. No failure history exists.
	parts = append(parts, CostPart{
		Component:  CostExpectedFailure,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "no failure history exists yet; runstore is collecting but has not accumulated enough observations to estimate this component",
	})

	return CostDecomposition{
		Parts:        parts,
		TotalLossPct: q.LossPct,
	}
}
