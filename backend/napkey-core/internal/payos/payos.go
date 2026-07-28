// Package payos implements the small subset of PayOS used by NapKey top-ups.
package payos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxResponseBody = 1 << 20

type CreatePaymentRequest struct {
	OrderCode   int64  `json:"orderCode"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
	CancelURL   string `json:"cancelUrl"`
	ReturnURL   string `json:"returnUrl"`
	Signature   string `json:"signature"`
}

type Checkout struct {
	Bin           string `json:"bin"`
	AccountNumber string `json:"accountNumber"`
	AccountName   string `json:"accountName"`
	Amount        int64  `json:"amount"`
	Description   string `json:"description"`
	OrderCode     int64  `json:"orderCode"`
	PaymentLinkID string `json:"paymentLinkId"`
	Status        string `json:"status"`
	CheckoutURL   string `json:"checkoutUrl"`
	QRCode        string `json:"qrCode"`
}

type Client struct {
	clientID    string
	apiKey      string
	checksumKey string
	endpoint    string
	httpClient  *http.Client
}

func NewClient(clientID, apiKey, checksumKey string) *Client {
	return &Client{
		clientID: strings.TrimSpace(clientID), apiKey: strings.TrimSpace(apiKey), checksumKey: checksumKey,
		endpoint: "https://api-merchant.payos.vn", httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.clientID != "" && c.apiKey != "" && c.checksumKey != ""
}

func (c *Client) CreatePayment(ctx context.Context, payment CreatePaymentRequest) (*Checkout, error) {
	if !c.Configured() {
		return nil, errors.New("payos: payment service is not configured")
	}
	payment.Signature = SignPaymentRequest(payment, c.checksumKey)
	body, err := json.Marshal(payment)
	if err != nil {
		return nil, fmt.Errorf("payos: encoding payment request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.endpoint, "/")+"/v2/payment-requests", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("payos: building payment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-client-id", c.clientID)
	req.Header.Set("x-api-key", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("payos: payment service is unavailable")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil || len(raw) > maxResponseBody {
		return nil, errors.New("payos: invalid payment service response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("payos: payment service rejected the request")
	}
	var envelope struct {
		Code string    `json:"code"`
		Data *Checkout `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Code != "00" || envelope.Data == nil {
		return nil, errors.New("payos: invalid payment service response")
	}
	if envelope.Data.OrderCode != payment.OrderCode || envelope.Data.Amount != payment.Amount || envelope.Data.CheckoutURL == "" {
		return nil, errors.New("payos: payment service returned mismatched checkout data")
	}
	checkoutURL, err := url.Parse(envelope.Data.CheckoutURL)
	if err != nil || checkoutURL.Scheme != "https" || (checkoutURL.Hostname() != "pay.payos.vn" && !strings.HasSuffix(checkoutURL.Hostname(), ".payos.vn")) {
		return nil, errors.New("payos: payment service returned an unsafe checkout URL")
	}
	return envelope.Data, nil
}

func SignPaymentRequest(payment CreatePaymentRequest, checksumKey string) string {
	canonical := "amount=" + strconv.FormatInt(payment.Amount, 10) +
		"&cancelUrl=" + payment.CancelURL +
		"&description=" + payment.Description +
		"&orderCode=" + strconv.FormatInt(payment.OrderCode, 10) +
		"&returnUrl=" + payment.ReturnURL
	return sign(canonical, checksumKey)
}

func SignWebhookData(data map[string]any, checksumKey string) (string, error) {
	canonical, err := canonicalData(data)
	if err != nil {
		return "", err
	}
	return sign(canonical, checksumKey), nil
}

func VerifyWebhookData(data map[string]any, signature, checksumKey string) error {
	want, err := SignWebhookData(data, checksumKey)
	if err != nil {
		return err
	}
	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return errors.New("payos: invalid webhook signature")
	}
	expected, _ := hex.DecodeString(want)
	if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
		return errors.New("payos: webhook signature mismatch")
	}
	return nil
}

func canonicalData(data map[string]any) (string, error) {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value, err := canonicalValue(data[key])
		if err != nil {
			return "", err
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "&"), nil
}

func canonicalValue(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("payos: canonicalizing webhook data: %w", err)
		}
		return string(raw), nil
	}
}

func sign(canonical, checksumKey string) string {
	mac := hmac.New(sha256.New, []byte(checksumKey))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}
