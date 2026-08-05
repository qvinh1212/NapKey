// Package pricing contains fixed-point arithmetic for NapKey billing.
//
// Money in NapKey is an int64 count of micro-VND, where 1 VND = 1,000,000 micros
// (DESIGN.md section 5). Credit measurements are converted from float64 once at
// the data-plane boundary; all stored quantities and charges are integers so a
// month of traffic reconciles exactly against its ledger rows.
package pricing

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// MicrosPerVND is the fixed-point scale for money.
const MicrosPerVND int64 = 1_000_000

// MicrocreditsPerCredit is the fixed-point scale for upstream credit usage.
const MicrocreditsPerCredit int64 = 1_000_000

const (
	RetailVNDPerCredit      int64 = 400
	UpstreamVNDPerCredit    int64 = 110
	RetailMicrosPerCredit         = RetailVNDPerCredit * MicrosPerVND
	UpstreamMicrosPerCredit       = UpstreamVNDPerCredit * MicrosPerVND
)

// tokensPerUnit is the denominator of every rate: prices are quoted per 1,000
// tokens.
const tokensPerUnit int64 = 1000

// FallbackModel is the sentinel model in model_prices used when a request's model
// has no price of its own. It is priced at the most expensive tier on purpose:
// serving an unrecognized model for free is a silent loss, while overcharging is
// visible and correctable.
const FallbackModel = "*"

// ErrOverflow means the arithmetic would exceed int64. It cannot happen with real
// traffic (a request would need ~10^12 tokens), so it indicates corrupt input
// rather than a large customer.
var ErrOverflow = errors.New("pricing: cost calculation overflows int64")

// CreditMicrosFromFloat converts the upstream measurement at the system boundary.
// The rest of billing uses integers so repeated aggregation cannot drift.
func CreditMicrosFromFloat(credits float64) (int64, error) {
	if math.IsNaN(credits) || math.IsInf(credits, 0) || credits < 0 {
		return 0, errors.New("pricing: credits must be a finite non-negative number")
	}
	if credits > float64(math.MaxInt64)/float64(MicrocreditsPerCredit) {
		return 0, ErrOverflow
	}
	return int64(math.Round(credits * float64(MicrocreditsPerCredit))), nil
}

// ComputeCreditCost prices microcredits at a micro-VND rate per whole credit.
func ComputeCreditCost(microcredits, microsPerCredit int64) (int64, error) {
	if microcredits < 0 || microsPerCredit < 0 {
		return 0, errors.New("pricing: credit quantities and rates cannot be negative")
	}
	if microcredits == 0 || microsPerCredit == 0 {
		return 0, nil
	}
	if microcredits > math.MaxInt64/microsPerCredit {
		return 0, ErrOverflow
	}
	return microcredits * microsPerCredit / MicrocreditsPerCredit, nil
}

// Rate is the price of one model over one period, in micro-VND per 1,000 tokens.
type Rate struct {
	// ID is the model_prices row this came from. Stored on each usage row so a
	// disputed charge can be traced back to the exact rate that produced it.
	ID                      int64
	Model                   string
	InputPer1k              int64
	OutputPer1k             int64
	CacheReadPer1k          int64
	CacheWritePer1k         int64
	UpstreamInputPer1k      int64
	UpstreamOutputPer1k     int64
	UpstreamCacheReadPer1k  int64
	UpstreamCacheWritePer1k int64
	// RequestFee is charged once per request, in micro-VND, on top of the token
	// rates. Coding-agent traffic is many small calls, so a token-only price
	// undercharges the fixed cost each upstream call carries regardless of size.
	// It also closes the reverse gap: without a token component a single huge
	// request would pay only the flat fee.
	RequestFee         int64
	UpstreamRequestFee int64
	EffectiveFrom      time.Time
	EffectiveTo        *time.Time
	SourceNote         string
}

func (r Rate) UpstreamRate() Rate {
	r.InputPer1k = r.UpstreamInputPer1k
	r.OutputPer1k = r.UpstreamOutputPer1k
	r.CacheReadPer1k = r.UpstreamCacheReadPer1k
	r.CacheWritePer1k = r.UpstreamCacheWritePer1k
	r.RequestFee = r.UpstreamRequestFee
	return r
}

// Tokens is one request's token counts, split by how they are billed.
//
// The four kinds are kept apart because their prices differ by more than an order
// of magnitude: a cache read is about a tenth of fresh input, while a cache write
// costs a premium over it. Summing them into one number, as the Stage 2 counter
// did, makes cache-heavy traffic unpriceable.
type Tokens struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Total is the sum across all four kinds, for display and for the token quota
// check. It is not used for pricing.
func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// IsZero reports whether the request consumed nothing.
func (t Tokens) IsZero() bool { return t.Total() == 0 }

// Cost is a priced request.
type Cost struct {
	// Micros is the total in micro-VND.
	Micros int64
	// Components breaks the total down by token kind, so the console can explain a
	// charge instead of presenting one opaque number.
	InputMicros      int64
	OutputMicros     int64
	CacheReadMicros  int64
	CacheWriteMicros int64
	// RequestFeeMicros is the flat per-request component of Micros.
	RequestFeeMicros int64
	// RateID is the model_prices row used. Zero means no price was found.
	RateID int64
	// Unpriced is true when no rate covered this model at this time. The request
	// was still served, so the row is recorded at zero cost and flagged for the
	// reconciliation job rather than dropped.
	Unpriced bool
}

// Compute prices a request against a rate.
//
// Each component is floored independently rather than the total being rounded.
// Flooring favors the customer, which is the required direction in DESIGN.md
// section 5: the amount a customer owes is never rounded up. The residue is at
// most 4 micros per request, or 0.000004 VND, which is not worth the reputational
// cost of appearing to round charges upward.
func Compute(t Tokens, r Rate) (Cost, error) {
	if err := validateTokens(t); err != nil {
		return Cost{}, err
	}
	input, err := lineCost(t.Input, r.InputPer1k)
	if err != nil {
		return Cost{}, fmt.Errorf("pricing: input tokens: %w", err)
	}
	output, err := lineCost(t.Output, r.OutputPer1k)
	if err != nil {
		return Cost{}, fmt.Errorf("pricing: output tokens: %w", err)
	}
	cacheRead, err := lineCost(t.CacheRead, r.CacheReadPer1k)
	if err != nil {
		return Cost{}, fmt.Errorf("pricing: cache read tokens: %w", err)
	}
	cacheWrite, err := lineCost(t.CacheWrite, r.CacheWritePer1k)
	if err != nil {
		return Cost{}, fmt.Errorf("pricing: cache write tokens: %w", err)
	}

	if r.RequestFee < 0 {
		return Cost{}, errors.New("pricing: request fee cannot be negative")
	}
	// The fee applies even to a request that reported no tokens: the upstream call
	// was still made and still cost money. Charging zero for it is the gap that
	// makes a token-only price exploitable.
	total, err := addAll(input, output, cacheRead, cacheWrite, r.RequestFee)
	if err != nil {
		return Cost{}, err
	}
	return Cost{
		Micros:           total,
		InputMicros:      input,
		OutputMicros:     output,
		CacheReadMicros:  cacheRead,
		CacheWriteMicros: cacheWrite,
		RequestFeeMicros: r.RequestFee,
		RateID:           r.ID,
	}, nil
}

// Unpriced is the result for a model with no rate on file: served, recorded, and
// charged nothing, with a flag that makes it findable.
func Unpriced() Cost { return Cost{Unpriced: true} }

// lineCost is floor(tokens * per1k / 1000) with an overflow guard.
//
// The multiplication is checked before it happens rather than detected afterward
// by looking for a sign flip, because a wrapped int64 is indistinguishable from a
// legitimate small number once it has wrapped.
func lineCost(tokens, per1k int64) (int64, error) {
	if tokens <= 0 || per1k <= 0 {
		return 0, nil
	}
	if tokens > math.MaxInt64/per1k {
		return 0, ErrOverflow
	}
	return tokens * per1k / tokensPerUnit, nil
}

func addAll(values ...int64) (int64, error) {
	var total int64
	for _, v := range values {
		if v > math.MaxInt64-total {
			return 0, ErrOverflow
		}
		total += v
	}
	return total, nil
}

// validateTokens rejects negative counts.
//
// A negative token count would produce a negative cost, which in Stage 4 becomes a
// credit to the wallet. That makes it an attack on the balance, not a data quality
// problem, so it is refused at the boundary rather than clamped silently.
func validateTokens(t Tokens) error {
	switch {
	case t.Input < 0:
		return errors.New("pricing: input tokens cannot be negative")
	case t.Output < 0:
		return errors.New("pricing: output tokens cannot be negative")
	case t.CacheRead < 0:
		return errors.New("pricing: cache read tokens cannot be negative")
	case t.CacheWrite < 0:
		return errors.New("pricing: cache write tokens cannot be negative")
	}
	return nil
}

// NormalizeModel canonicalizes a model id for price lookup.
//
// Upstream model ids arrive in mixed case and occasionally with surrounding
// whitespace. model_prices stores lowercase, enforced by a check constraint, so
// normalizing here keeps the lookup from missing a price over capitalization.
func NormalizeModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// VNDFromMicros converts micros to whole VND, rounding down.
//
// Down, always: this renders both a balance and an amount owed, and rounding a
// balance up would show money that is not there. Truncation toward zero and floor
// agree here because money in this system is never negative.
func VNDFromMicros(micros int64) int64 { return micros / MicrosPerVND }

// MicrosFromVND converts whole VND to micros.
func MicrosFromVND(vnd int64) (int64, error) {
	if vnd > math.MaxInt64/MicrosPerVND || vnd < math.MinInt64/MicrosPerVND {
		return 0, ErrOverflow
	}
	return vnd * MicrosPerVND, nil
}

// FormatVND renders micros as a human-readable VND amount with thousands
// separators, e.g. 1234567890 -> "1.234". Vietnamese convention uses a dot as the
// thousands separator.
//
// The fractional dong is dropped rather than shown: a price of 0.078 VND per token
// is meaningful in aggregate but noise on a single line, and showing six decimal
// places invites the reader to think the last digit matters.
func FormatVND(micros int64) string {
	vnd := VNDFromMicros(micros)
	negative := vnd < 0
	if negative {
		vnd = -vnd
	}
	digits := fmt.Sprintf("%d", vnd)
	var b strings.Builder
	if negative {
		b.WriteByte('-')
	}
	for i, ch := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(ch)
	}
	return b.String()
}
