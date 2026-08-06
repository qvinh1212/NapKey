package proxy

// Detecting upstream prompt overhead.
//
// The 9Router upstream prepends its own instructions to every request. Measured on the
// live endpoint, a one-word prompt is reported as ~4,541 input tokens, and a prompt 500
// words longer is reported as 498 tokens more: the difference tracks the caller's text
// exactly, so the remainder is a fixed block the upstream adds.
//
// That block is a real cost — the upstream bills us for it — so it is billed to the
// customer rather than absorbed. What is not acceptable is that it be invisible: a
// customer sending "Hi" sees 4,541 input tokens with no explanation, and support has no
// way to tell an inflated bill from a legitimate one.
//
// So the gap is measured and logged rather than corrected. Nothing here changes what is
// charged. The number is deliberately not hardcoded: it is the upstream's business and
// can change without notice, so it is derived per request by comparing what the caller
// sent against what the upstream reported.

import (
	"kiro-go/logger"
)

// overheadReportThreshold is how many unexplained input tokens are worth logging.
//
// Well above the estimator's own error, which is a token or two on a short prompt, so a
// normal request never trips this. Low enough that a systemic block of thousands cannot
// hide underneath it.
const overheadReportThreshold = 500

// reportPromptOverhead logs the gap between the tokens a caller sent and the tokens the
// upstream billed for.
//
// estimatedInput is this process's own estimate of the caller's payload; reportedInput
// is what the upstream charged. A large positive gap means the upstream added a prompt
// of its own, and the customer is paying for it.
//
// Logged at info, not warn: this is expected behaviour for this upstream, and a warning
// on every request would train operators to ignore warnings. It exists so the overhead
// is auditable and so a sudden change in its size is visible.
func reportPromptOverhead(model string, estimatedInput, reportedInput int) {
	overhead := promptOverhead(estimatedInput, reportedInput)
	if overhead == 0 {
		return
	}
	share := float64(overhead) / float64(reportedInput) * 100
	logger.Infof(
		"[9Router] upstream prompt overhead for %s: billed %d input tokens, caller sent ~%d, "+
			"overhead ~%d (%.0f%% of the charge)",
		model, reportedInput, estimatedInput, overhead, share,
	)
}

// promptOverhead returns the unexplained input tokens, or zero when there is no finding.
//
// Split out from the logging so the decision is testable on its own. Returns zero rather
// than a negative number when the upstream bills less than the estimate, which happens
// when it tokenises more efficiently than the estimator guesses and is not a finding.
func promptOverhead(estimatedInput, reportedInput int) int {
	if estimatedInput <= 0 || reportedInput <= 0 {
		return 0
	}
	overhead := reportedInput - estimatedInput
	if overhead < overheadReportThreshold {
		return 0
	}
	return overhead
}

// outputBudgetOvershootRatio is how far past max_tokens a response must run before it
// is worth logging.
//
// Not 1.0. A handful of tokens over the limit is a tokeniser disagreement between this
// process and the upstream, not a broken cap, and logging it would bury the real cases.
// A response 25% longer than the caller allowed cannot be explained that way.
const outputBudgetOvershootRatio = 1.25

// reportOutputBudgetOvershoot logs a response that ran past the caller's max_tokens.
//
// The upstream does not enforce max_tokens. Measured on the live endpoint on
// 2026-08-06: a request capped at 600 tokens came back with 751 to 1,431 depending on
// the model, so the cap is advisory at best and absent at worst.
//
// This matters to a customer rather than to margin. Output is billed at the same rate
// as input, so a caller who sets a budget to bound their spend does not actually get
// one, and the overage is charged to them. NapKey cannot fix that from here -- truncating
// the response would mean charging for tokens the customer never receives, which is
// worse -- but a bill nobody can explain is the thing support cannot answer, so the
// overshoot is recorded per request.
//
// budget is the caller's max_tokens, zero when they set none. reported is what the
// upstream billed for output. Logged at warn rather than info, unlike prompt overhead:
// the injected prompt is this upstream's normal behaviour, while ignoring an explicit
// limit is a contract the caller thinks they have and does not.
func reportOutputBudgetOvershoot(model string, budget, reported int) {
	if !outputBudgetExceeded(budget, reported) {
		return
	}
	logger.Warnf(
		"[9Router] upstream ignored max_tokens for %s: caller allowed %d output tokens, "+
			"upstream billed %d (%.0f%% over); the customer pays the difference",
		model, budget, reported, float64(reported-budget)/float64(budget)*100,
	)
}

// outputBudgetExceeded reports whether a response ran materially past its budget.
//
// Split out from the logging so the threshold is testable on its own. A caller who set
// no budget cannot have one exceeded, so a zero or negative budget is never a finding.
func outputBudgetExceeded(budget, reported int) bool {
	if budget <= 0 || reported <= 0 {
		return false
	}
	return float64(reported) > float64(budget)*outputBudgetOvershootRatio
}
