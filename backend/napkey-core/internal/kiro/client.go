// Package kiro talks to the kiro-go data plane's admin API.
//
// The contract from DESIGN.md section 4: napkey-core is the only writer of API
// keys, and it pushes them to kiro-go over /admin/api/api-keys. kiro-go stays the
// component that authenticates proxied traffic, so a key only works once this
// push has landed.
package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client calls the kiro-go admin API.
type Client struct {
	baseURL  string
	password string
	http     *http.Client
}

// ErrNotFound means kiro-go has no such key, which for a delete is success.
var ErrNotFound = errors.New("kiro: resource not found")

// ErrUnauthorized means the admin password was rejected.
var ErrUnauthorized = errors.New("kiro: admin password rejected")

// New builds a client. The timeout is short because these calls sit in the path
// of a user creating a key; a slow data plane should surface as a pending sync,
// not a hung request.
func New(baseURL, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				MaxIdleConns:          10,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
			},
		},
	}
}

// KeyView mirrors kiro-go's apiKeyView response shape.
type KeyView struct {
	ID            string  `json:"id"`
	Name          string  `json:"name,omitempty"`
	KeyMasked     string  `json:"keyMasked"`
	Enabled       bool    `json:"enabled"`
	CreatedAt     int64   `json:"createdAt"`
	LastUsedAt    int64   `json:"lastUsedAt,omitempty"`
	RPMLimit      int     `json:"rpmLimit,omitempty"`
	TPMLimit      int     `json:"tpmLimit,omitempty"`
	TokenLimit    int64   `json:"tokenLimit,omitempty"`
	CreditLimit   float64 `json:"creditLimit,omitempty"`
	TokensUsed    int64   `json:"tokensUsed"`
	CreditsUsed   float64 `json:"creditsUsed"`
	RequestsCount int64   `json:"requestsCount"`
}

type UsageReportingStatus struct {
	Enabled   int64 `json:"enabled"`
	Healthy   int64 `json:"healthy"`
	Queued    int64 `json:"queued"`
	Sent      int64 `json:"sent"`
	Duplicate int64 `json:"duplicate"`
	Dropped   int64 `json:"dropped"`
	Pending   int64 `json:"pending"`
}

type OperationsStatus struct {
	Version         string               `json:"version"`
	Accounts        int                  `json:"accounts"`
	Available       int                  `json:"available"`
	TotalRequests   int64                `json:"totalRequests"`
	SuccessRequests int64                `json:"successRequests"`
	FailedRequests  int64                `json:"failedRequests"`
	RecentRequests  int64                `json:"recentRequests"`
	RecentFailures  int64                `json:"recentFailures"`
	TotalTokens     int64                `json:"totalTokens"`
	Uptime          int64                `json:"uptime"`
	UsageReporting  UsageReportingStatus `json:"usageReporting"`
}

func (c *Client) OperationsStatus(ctx context.Context) (*OperationsStatus, error) {
	var status OperationsStatus
	if err := c.do(ctx, http.MethodGet, "/admin/api/status", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// CreateKeyRequest is the body for POST /admin/api/api-keys.
//
// Key is sent explicitly: napkey-core generates the value so it can hash it before
// storing, and letting kiro-go generate one instead would leave the control plane
// without the cleartext to show the user.
type CreateKeyRequest struct {
	Name        string  `json:"name,omitempty"`
	Key         string  `json:"key"`
	Enabled     *bool   `json:"enabled,omitempty"`
	RPMLimit    int     `json:"rpmLimit,omitempty"`
	TPMLimit    int     `json:"tpmLimit,omitempty"`
	TokenLimit  int64   `json:"tokenLimit,omitempty"`
	CreditLimit float64 `json:"creditLimit,omitempty"`
}

type createKeyResponse struct {
	Success bool    `json:"success"`
	ID      string  `json:"id"`
	Key     string  `json:"key"`
	APIKey  KeyView `json:"apiKey"`
	Error   string  `json:"error"`
}

// CreateKey pushes a new key and returns the id kiro-go assigned.
func (c *Client) CreateKey(ctx context.Context, req CreateKeyRequest) (string, error) {
	var resp createKeyResponse
	err := c.do(ctx, http.MethodPost, "/admin/api/api-keys", req, &resp)
	if err != nil {
		// kiro-go rejects a duplicate key value. That means the data plane already
		// holds this exact key, so the desired state is satisfied and treating it
		// as failure would retry forever.
		if isAlreadyExists(err) {
			return "", nil
		}
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("kiro: creating key: %s", resp.Error)
	}
	if resp.ID == "" {
		return "", errors.New("kiro: create key response had no id")
	}
	return resp.ID, nil
}

// UpdateKeyRequest is the body for PUT /admin/api/api-keys/{id}.
type UpdateKeyRequest struct {
	Name        *string  `json:"name,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	RPMLimit    *int     `json:"rpmLimit,omitempty"`
	TPMLimit    *int     `json:"tpmLimit,omitempty"`
	TokenLimit  *int64   `json:"tokenLimit,omitempty"`
	CreditLimit *float64 `json:"creditLimit,omitempty"`
}

// UpdateKey applies an edit to an existing key in kiro-go.
func (c *Client) UpdateKey(ctx context.Context, remoteID string, req UpdateKeyRequest) error {
	if remoteID == "" {
		return errors.New("kiro: update requires a remote id")
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := c.do(ctx, http.MethodPut, "/admin/api/api-keys/"+remoteID, req, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("kiro: updating key: %s", resp.Error)
	}
	return nil
}

// DeleteKey removes a key from kiro-go. A missing key counts as success, since the
// goal is that the key no longer authenticates.
func (c *Client) DeleteKey(ctx context.Context, remoteID string) error {
	if remoteID == "" {
		return errors.New("kiro: delete requires a remote id")
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	err := c.do(ctx, http.MethodDelete, "/admin/api/api-keys/"+remoteID, nil, &resp)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// ListKeys reads every key kiro-go holds, used to reconcile drift.
func (c *Client) ListKeys(ctx context.Context) ([]KeyView, error) {
	var resp struct {
		APIKeys []KeyView `json:"apiKeys"`
		Error   string    `json:"error"`
	}
	if err := c.do(ctx, http.MethodGet, "/admin/api/api-keys", nil, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("kiro: listing keys: %s", resp.Error)
	}
	return resp.APIKeys, nil
}

// Health checks that kiro-go is reachable and the admin password works.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.ListKeys(ctx)
	return err
}

// maxResponseBytes caps how much of a reply is read. A data plane returning an
// unexpected HTML error page should not be able to exhaust memory here.
const maxResponseBytes = 4 << 20

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kiro: encoding request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("kiro: building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// kiro-go accepts the admin password in this header or a cookie. The header is
	// used so no credential ends up in a cookie jar shared across requests.
	req.Header.Set("X-Admin-Password", c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kiro: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("kiro: reading response from %s %s: %w", method, path, err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 300:
		var known struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &known) == nil && known.Error == "api key already exists" {
			return fmt.Errorf("kiro: api key already exists")
		}
		return fmt.Errorf("kiro: %s %s returned %d", method, path, resp.StatusCode)
	}

	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("kiro: decoding response from %s %s: %w", method, path, err)
	}
	return nil
}

func isAlreadyExists(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists")
}
