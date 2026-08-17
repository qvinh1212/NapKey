package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

type walletAuthorizationRequest struct {
	KeyID string `json:"keyId"`
	RequestID string `json:"requestId"`
	Model string `json:"model"`
	InputTokens int64 `json:"inputTokens"`
	MaxOutputTokens int64 `json:"maxOutputTokens"`
}

// walletHoldCeilingVND caps what a single request may reserve, so one call cannot
// take a whole wallet out of circulation while it is in flight.
//
// In VND rather than in credits. It was 10 credits until the retail rate returned to
// 75 in migration 0022, which would have quietly cut the ceiling from 4,000 VND to
// 750 VND and started refusing requests that are served today -- the price page
// quotes 948 VND for a large-context call, and a 200k-token request with a 32k cap
// quotes over 3,000. The ceiling is a risk control on money in flight; the retail
// credit rate is a display unit for money held. Tying the first to the second meant
// every change to how a balance is shown resized the limit on what may be reserved.
const walletHoldCeilingVND int64 = 4_000

// The billable size the upstream adds to a request on top of what the caller sent.
//
// A hold is quoted from the caller's own numbers; settlement is priced from what the
// upstream reports. Those disagree systematically here, in the same direction every time,
// so a hold taken at face value comes up short. Two causes, both measured against the
// live upstream on 2026-08-06:
//
//	it prepends its own prompt and bills for it -- 2,000 to 2,623 tokens depending on
//	which backend answers, which no caller declares because no caller sends it
//
//	it does not enforce max_tokens -- a request capped at 100 output tokens was billed
//	for 1,121, so the cap bounds the hold but not the charge
//
// The input figure is added rather than used as a floor, because the injected prompt is
// charged in addition to the caller's text: a 50,000-token prompt was billed at 52,600.
// A floor would cover a short prompt and do nothing for a long one, which is how the
// worst-affected shape -- a large prompt with a small cap -- stayed exposed.
//
// The output figure is a floor rather than an addition, because it stands in for a cap
// that is not applied at all. What comes back is a property of the model and the
// question, not of the cap, so the floor sits above every output length measured while
// the cap was being ignored.
//
// Neither is a percentage. Both are fixed blocks of tokens that do not scale with request
// size, and a percentage would over-reserve a large request while still under-reserving a
// small one -- exactly backwards.
const (
	upstreamInjectedInputTokens int64 = 2_623
	upstreamMinimumOutputTokens int64 = 1_500
)

// walletHoldTokens converts a caller's declared size into the size to hold against.
//
// Applied to token counts rather than to the finished quote, because the shortfall is
// entirely in the token component. The per-request fee is identical on both sides -- the
// hold carries it and so does settlement -- so comparing whole quotes hides the gap behind
// a constant: a 301 VND hold against a 337 VND charge differs by 36 VND of tokens while
// both include the same 300 VND fee.
func walletHoldTokens(declared pricing.Tokens) pricing.Tokens {
	held := declared
	held.Input += upstreamInjectedInputTokens
	if held.Output < upstreamMinimumOutputTokens {
		held.Output = upstreamMinimumOutputTokens
	}
	return held
}

// walletHoldAmount clamps a quoted hold: never below a whole VND, never above the ceiling
// that stops one request reserving an entire wallet.
//
// Over-reserving is the safe direction. A hold is released or settled within the same
// request, so reserving more than the charge only briefly reduces what else the wallet can
// authorise; reserving less means a request that has already been served cannot be paid
// for, which leaves the hold open until it expires and the balance wrong until someone
// notices.
func walletHoldAmount(quotedMicros int64) int64 {
	minimum := pricing.MicrosPerVND
	maximum := walletHoldCeilingVND * pricing.MicrosPerVND
	if quotedMicros < minimum {
		return minimum
	}
	if quotedMicros > maximum {
		return maximum
	}
	return quotedMicros
}

func (s *Server) handleReserveWallet(w http.ResponseWriter,r *http.Request){
	var req walletAuthorizationRequest;if !decodeJSON(w,r,&req){return}
	req.KeyID=strings.TrimSpace(req.KeyID);req.RequestID=strings.TrimSpace(req.RequestID)
	if req.KeyID==""||req.RequestID==""{writeError(w,http.StatusBadRequest,codeInvalidRequest,"keyId and requestId are required");return}
	if len(req.RequestID)>200{writeError(w,http.StatusBadRequest,codeInvalidRequest,"requestId is too long");return}
	if req.InputTokens<0||req.MaxOutputTokens<0||(req.InputTokens==0&&req.MaxOutputTokens==0)||req.InputTokens>10_000_000||req.MaxOutputTokens>10_000_000{writeError(w,http.StatusBadRequest,codeInvalidRequest,"token estimate must be between 0 and 10,000,000");return}
	rate,err:=s.store.FindRate(r.Context(),req.Model,time.Now());if err!=nil{writeStoreError(w,err,"pricing wallet hold");return};if rate==nil{writeError(w,http.StatusServiceUnavailable,codeInternal,"model has no configured wallet price");return}
	// Priced from the caller's declared size, raised to what the upstream is known to bill
	// on top of it: it prepends a prompt nobody declares and does not enforce max_tokens,
	// so a hold taken at face value settles short.
	quote,err:=pricing.Compute(walletHoldTokens(pricing.Tokens{Input:req.InputTokens,Output:req.MaxOutputTokens}),*rate);if err!=nil{writeError(w,http.StatusBadRequest,codeInvalidRequest,"token estimate is too large");return}
	// Checked after the allowance is applied, so a request cannot be admitted on a declared
	// size it will not be billed at.
	if quote.Micros>s.cfg.WalletHoldMicros{writeError(w,http.StatusBadRequest,codeInvalidRequest,"request exceeds the configured wallet hold ceiling");return}
	amount:=walletHoldAmount(quote.Micros)
	hold,err:=s.store.ReserveWalletForKey(r.Context(),req.KeyID,req.RequestID,amount)
	if errors.Is(err,store.ErrInsufficientFunds){writeError(w,http.StatusPaymentRequired,codeForbidden,"wallet balance is insufficient");return}
	if err!=nil{writeStoreError(w,err,"reserving wallet balance");return}
	writeJSON(w,http.StatusOK,map[string]any{"status":"reserved","requestId":hold.RequestID,"amountMicros":hold.AmountMicros,"expiresAt":hold.ExpiresAt})
}

func (s *Server) handleReleaseWallet(w http.ResponseWriter,r *http.Request){
	var req struct{RequestID string `json:"requestId"`};if !decodeJSON(w,r,&req){return};req.RequestID=strings.TrimSpace(req.RequestID)
	if req.RequestID==""{writeError(w,http.StatusBadRequest,codeInvalidRequest,"requestId is required");return}
	err:=s.store.ReleaseWallet(r.Context(),req.RequestID);if errors.Is(err,store.ErrNotFound){writeJSON(w,http.StatusOK,map[string]string{"status":"not_found"});return};if err!=nil{writeStoreError(w,err,"releasing wallet balance");return}
	writeJSON(w,http.StatusOK,map[string]string{"status":"released"})
}
