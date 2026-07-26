package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"napkey-core/internal/casso"
	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

const maxCassoBody = 1 << 20

func (s *Server) handleGetWallet(w http.ResponseWriter,r *http.Request){
	su:=sessionFromContext(r.Context()); wallet,err:=s.store.GetWallet(r.Context(),su.User.ID)
	if err!=nil{writeStoreError(w,err,"loading wallet");return}
	writeJSON(w,http.StatusOK,map[string]any{"wallet":map[string]any{"balance":costView(wallet.BalanceMicros),"held":costView(wallet.HeldMicros),"available":costView(wallet.BalanceMicros-wallet.HeldMicros),"currency":wallet.Currency}})
}

func (s *Server) handleCreateTopup(w http.ResponseWriter,r *http.Request){
	var req struct{AmountVND int64 `json:"amountVnd"`};if !decodeJSON(w,r,&req){return}
	if req.AmountVND<20_000||req.AmountVND>1_000_000_000{writeError(w,http.StatusBadRequest,codeInvalidRequest,"amountVnd must be between 20,000 and 1,000,000,000");return}
	amount,err:=pricing.MicrosFromVND(req.AmountVND);if err!=nil{writeError(w,http.StatusBadRequest,codeInvalidRequest,"amountVnd is too large");return}
	if s.cfg.BankAccountNumber==""{writeError(w,http.StatusServiceUnavailable,codeInternal,"bank transfer is not configured");return}
	su:=sessionFromContext(r.Context());order,err:=s.store.CreateTopupOrder(r.Context(),su.User.ID,amount,s.cfg.BankAccountNumber)
	if err!=nil{writeStoreError(w,err,"creating top-up order");return}
	writeJSON(w,http.StatusCreated,s.topupView(order))
}

func (s *Server) handleGetTopup(w http.ResponseWriter,r *http.Request){
	su:=sessionFromContext(r.Context());order,err:=s.store.GetTopupOrder(r.Context(),su.User.ID,r.PathValue("id"))
	if err!=nil{writeStoreError(w,err,"loading top-up order");return};writeJSON(w,http.StatusOK,s.topupView(order))
}

func (s *Server) topupView(order *store.TopupOrder)map[string]any{
	return map[string]any{"order":map[string]any{"id":order.ID,"memoCode":order.MemoCode,"status":order.Status,"expectedAmount":costView(order.ExpectedAmountMicros),"receivedAmount":costView(order.ReceivedAmountMicros),"expiresAt":order.ExpiresAt.UTC().Format(time.RFC3339),"paidAt":formatOptionalTime(order.PaidAt),"bank":map[string]any{"name":s.cfg.BankName,"bin":s.cfg.BankBin,"accountNumber":order.BankAccountNumber,"accountName":s.cfg.BankAccountName}}}
}

func formatOptionalTime(value *time.Time)any{if value==nil{return nil};return value.UTC().Format(time.RFC3339)}

// handleCassoWebhook verifies and journals only; the payment worker credits later.
func (s *Server) handleCassoWebhook(w http.ResponseWriter,r *http.Request){
	raw,err:=io.ReadAll(io.LimitReader(r.Body,maxCassoBody+1));if err!=nil{writeError(w,http.StatusBadRequest,codeInvalidRequest,"could not read webhook body");return};if len(raw)>maxCassoBody{writeError(w,http.StatusRequestEntityTooLarge,codeInvalidRequest,"webhook body exceeds 1 MiB");return}
	payload:=json.RawMessage(raw);if !json.Valid(raw){wrapped,_:=json.Marshal(map[string]string{"rawBase64":base64.StdEncoding.EncodeToString(raw)});payload=wrapped}
	tx,parseErr:=casso.ParseTransaction(raw);providerID:=tx.ID
	if providerID==""{sum:=sha256.Sum256(raw);providerID="rejected:"+hex.EncodeToString(sum[:])}
	verifiedErr:=casso.VerifySignature(raw,r.Header.Get("X-Casso-Signature"),s.cfg.CassoWebhookSecret,time.Now())
	status:="received";message:="";verified:=verifiedErr==nil
	if verifiedErr!=nil{status="rejected";message=verifiedErr.Error()}else if parseErr!=nil{status="rejected";message=parseErr.Error()}
	if _,_,err = s.store.InsertPaymentEvent(r.Context(),store.PaymentEventInput{ProviderTxID:providerID,BankReference:tx.Reference,SignatureVerified:verified,Payload:payload,Status:status,ErrorMessage:message});err!=nil{
		writeError(w,http.StatusServiceUnavailable,codeInternal,"could not journal payment event")
		return
	}
	w.Header().Set("Content-Type","application/json");w.WriteHeader(http.StatusOK);_,_=w.Write([]byte(`{"success":1}`))
}
