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

const walletHoldCredits int64 = 10

func walletHoldAmount(quotedMicros int64) int64 {
	minimum := pricing.MicrosPerVND
	maximum := walletHoldCredits * pricing.RetailMicrosPerCredit
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
	quote,err:=pricing.Compute(pricing.Tokens{Input:req.InputTokens,Output:req.MaxOutputTokens},*rate);if err!=nil{writeError(w,http.StatusBadRequest,codeInvalidRequest,"token estimate is too large");return}
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
