package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"napkey-core/internal/casso"
	"napkey-core/internal/payos"
	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

const maxCassoBody = 1 << 20

func (s *Server) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	wallet, err := s.store.GetWallet(r.Context(), su.User.ID)
	if err != nil {
		writeStoreError(w, err, "loading wallet")
		return
	}
	available := wallet.BalanceMicros - wallet.HeldMicros
	writeJSON(w, http.StatusOK, map[string]any{"wallet": map[string]any{"balance": costView(wallet.BalanceMicros), "held": costView(wallet.HeldMicros), "available": costView(available), "credits": map[string]any{"balance": creditsView(wallet.BalanceMicros / pricing.RetailVNDPerCredit), "held": creditsView(wallet.HeldMicros / pricing.RetailVNDPerCredit), "available": creditsView(available / pricing.RetailVNDPerCredit), "promotional": creditsView(wallet.PromotionalMicros / pricing.RetailVNDPerCredit), "promotionalExpiresAt": formatOptionalTime(wallet.PromotionalExpiresAt), "vndPerCredit": pricing.RetailVNDPerCredit}, "currency": wallet.Currency}})
}

func (s *Server) handleCreateTopup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AmountVND int64 `json:"amountVnd"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AmountVND < store.MinTopupVND || req.AmountVND > 1_000_000_000 || req.AmountVND%store.TopupStepVND != 0 {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "amountVnd must be between 10,000 and 1,000,000,000 in 1,000 VND increments")
		return
	}
	amount, err := pricing.MicrosFromVND(req.AmountVND)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "amountVnd is too large")
		return
	}
	if !s.payos.Configured() {
		writeError(w, http.StatusServiceUnavailable, codeInternal, "PayOS is not configured")
		return
	}
	su := sessionFromContext(r.Context())
	order, err := s.store.CreateTopupOrder(r.Context(), su.User.ID, amount)
	if err != nil {
		writeStoreError(w, err, "creating top-up order")
		return
	}
	checkout, err := s.payos.CreatePayment(r.Context(), payos.CreatePaymentRequest{
		OrderCode: order.ProviderOrderCode, Amount: req.AmountVND, Description: "NAPKEY " + order.MemoCode,
		CancelURL: s.cfg.PublicBaseURL + "/vi/console/wallet?payment=cancelled",
		ReturnURL: s.cfg.PublicBaseURL + "/vi/console/wallet?payment=success",
	})
	if err != nil {
		_ = s.store.CancelTopupOrder(r.Context(), su.User.ID, order.ID)
		writeError(w, http.StatusBadGateway, codeUpstreamFailure, "could not create the PayOS checkout")
		return
	}
	order, err = s.store.AttachPayOSCheckout(r.Context(), su.User.ID, order.ID, checkout.PaymentLinkID, checkout.CheckoutURL, checkout.QRCode)
	if err != nil {
		writeStoreError(w, err, "saving PayOS checkout")
		return
	}
	writeJSON(w, http.StatusCreated, s.topupView(order))
}

func (s *Server) handleGetTopup(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	order, err := s.store.GetTopupOrder(r.Context(), su.User.ID, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "loading top-up order")
		return
	}
	writeJSON(w, http.StatusOK, s.topupView(order))
}

func (s *Server) handleListTopups(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	orders, err := s.store.ListTopupOrders(r.Context(), su.User.ID, 50)
	if err != nil {
		writeStoreError(w, err, "loading top-up history")
		return
	}
	items := make([]map[string]any, 0, len(orders))
	for i := range orders {
		items = append(items, s.topupView(&orders[i])["order"].(map[string]any))
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": items})
}

func (s *Server) topupView(order *store.TopupOrder) map[string]any {
	return map[string]any{"order": map[string]any{"id": order.ID, "memoCode": order.MemoCode, "status": order.Status, "expectedAmount": costView(order.ExpectedAmountMicros), "expectedCredits": creditsView(order.ExpectedAmountMicros / order.RetailVNDPerCredit), "receivedAmount": costView(order.ReceivedAmountMicros), "expiresAt": order.ExpiresAt.UTC().Format(time.RFC3339), "paidAt": formatOptionalTime(order.PaidAt), "createdAt": order.CreatedAt.UTC().Format(time.RFC3339), "payment": map[string]any{"provider": order.Provider, "checkoutUrl": order.CheckoutURL, "qrCode": order.QRCode}}}
}

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func (s *Server) handlePayOSWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PayOSChecksumKey == "" {
		writeError(w, http.StatusServiceUnavailable, codeInternal, "PayOS is not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCassoBody+1))
	if err != nil || len(raw) > maxCassoBody {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "invalid PayOS webhook body")
		return
	}
	var envelope struct {
		Code      string         `json:"code"`
		Success   bool           `json:"success"`
		Data      map[string]any `json:"data"`
		Signature string         `json:"signature"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil || envelope.Data == nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "invalid PayOS webhook payload")
		return
	}
	if err := payos.VerifyWebhookData(envelope.Data, envelope.Signature, s.cfg.PayOSChecksumKey); err != nil {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "invalid PayOS webhook signature")
		return
	}
	if !envelope.Success || envelope.Code != "00" {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
	orderCode, ok := payOSInt64(envelope.Data["orderCode"])
	if !ok { writeError(w,http.StatusBadRequest,codeInvalidRequest,"PayOS orderCode is invalid");return }
	amountVND, ok := payOSInt64(envelope.Data["amount"])
	if !ok || amountVND <= 0 { writeError(w,http.StatusBadRequest,codeInvalidRequest,"PayOS amount is invalid");return }
	providerTxID, ok := payOSTransactionID(envelope.Data)
	if !ok || orderCode <= 0 { writeError(w,http.StatusBadRequest,codeInvalidRequest,"PayOS transaction identity is invalid");return }
	amountMicros, err := pricing.MicrosFromVND(amountVND)
	if err != nil { writeError(w,http.StatusBadRequest,codeInvalidRequest,"PayOS amount is outside the supported range");return }
	if _, err := s.store.CreditPayOSPayment(r.Context(), store.PayOSPaymentInput{ProviderTxID:providerTxID,OrderCode:orderCode,AmountMicros:amountMicros,Payload:raw}); err != nil {
		writeError(w,http.StatusServiceUnavailable,codeInternal,"could not record PayOS payment");return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func payOSTransactionID(data map[string]any) (string, bool) {
	paymentLinkID, linkOK := data["paymentLinkId"].(string)
	reference, referenceOK := data["reference"].(string)
	paymentLinkID = strings.TrimSpace(paymentLinkID)
	reference = strings.TrimSpace(reference)
	if !linkOK || !referenceOK || paymentLinkID == "" || reference == "" {
		return "", false
	}
	return paymentLinkID + ":" + reference, true
}

func payOSInt64(value any)(int64,bool){
	switch v:=value.(type){case json.Number:n,err:=strconv.ParseInt(v.String(),10,64);return n,err==nil;case float64:return int64(v),v==float64(int64(v));default:return 0,false}
}

// handleCassoWebhook verifies and journals only; the payment worker credits later.
func (s *Server) handleCassoWebhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCassoBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "could not read webhook body")
		return
	}
	if len(raw) > maxCassoBody {
		writeError(w, http.StatusRequestEntityTooLarge, codeInvalidRequest, "webhook body exceeds 1 MiB")
		return
	}
	payload := json.RawMessage(raw)
	if !json.Valid(raw) {
		wrapped, _ := json.Marshal(map[string]string{"rawBase64": base64.StdEncoding.EncodeToString(raw)})
		payload = wrapped
	}
	tx, parseErr := casso.ParseTransaction(raw)
	providerID := tx.ID
	if providerID == "" {
		sum := sha256.Sum256(raw)
		providerID = "rejected:" + hex.EncodeToString(sum[:])
	}
	verifiedErr := casso.VerifySignature(raw, r.Header.Get("X-Casso-Signature"), s.cfg.CassoWebhookSecret, time.Now())
	status := "received"
	message := ""
	verified := verifiedErr == nil
	if verifiedErr != nil {
		status = "rejected"
		message = verifiedErr.Error()
	} else if parseErr != nil {
		status = "rejected"
		message = parseErr.Error()
	}
	if _, _, err = s.store.InsertPaymentEvent(r.Context(), store.PaymentEventInput{ProviderTxID: providerID, BankReference: tx.Reference, SignatureVerified: verified, Payload: payload, Status: status, ErrorMessage: message}); err != nil {
		writeError(w, http.StatusServiceUnavailable, codeInternal, "could not journal payment event")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"success":1}`))
}
