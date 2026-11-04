// spidi.go - Cliente HTTP contra la API de SPIDI con token management.
package spidi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	sandboxBase    = "https://sandbox.api.spidipagos.com"
	productionBase = "https://api.spidipagos.com"
	defaultTimeout = 30 * time.Second
	tokenLeeway    = 5 * time.Minute // refrescar token 5 min antes de expirar
)

// Client maneja la comunicación con SPIDI API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	tokenExp   time.Time
	mu         sync.RWMutex
	logger     *slog.Logger
}

// TokenResponse es la respuesta del login SPIDI.
type TokenResponse struct {
	Token string `json:"token"`
}

// AgreementResponse es la respuesta al crear un agreement.
type AgreementResponse struct {
	Success bool `json:"success"`
	Data    struct {
		AgreementID         string `json:"agreement_id"`
		Title               string `json:"title"`
		Split               bool   `json:"split"`
		DefaultBankAccountID string `json:"default_bank_account_id"`
		Status              string `json:"status"`
		CreatedAt          string `json:"created_at"`
	} `json:"data"`
}

// SessionResponse es la respuesta al crear una sesión de pago.
type SessionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		SessionID        string  `json:"session_id"`
		PaymentURL       string  `json:"payment_url"`
		Currency         string  `json:"currency_reference"`
		Amount           float64 `json:"amount_reference"`
		OrderID          string  `json:"identifier"`
		Status           string  `json:"status"`
		DueDateSession   string  `json:"due_date_session"`
	} `json:"data"`
}

// SessionStatus es el estado consultado de una sesión.
type SessionStatus struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		SessionID      string  `json:"session_id"`
		Status         string  `json:"status"`
		Currency       string  `json:"currency_reference"`
		Amount         float64 `json:"amount_reference"`
		OrderID        string  `json:"identifier"`
		PaymentMethod  string  `json:"payment_method,omitempty"`
		TransactionID  string  `json:"spidi_transaction_id,omitempty"`
		TransactionURL string  `json:"spidi_transaction_url,omitempty"`
		PaidAt         string  `json:"paid_at,omitempty"`
		DueDate        string  `json:"due_date_session,omitempty"`
	} `json:"data"`
}

// SPIDIESpecificError es un error de la API SPIDI.
type SPIDIESpecificError struct {
	StatusCode int
	Message    string
	Raw        string
}

func (e *SPIDIESpecificError) Error() string {
	return fmt.Sprintf("spidi: http %d: %s", e.StatusCode, e.Message)
}

// NewClient crea un cliente para SPIDI.
func NewClient(environment string, logger *slog.Logger) *Client {
	base := sandboxBase
	if environment == "production" {
		base = productionBase
	}
	return &Client{
		baseURL: base,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// Login obtiene y cachea el token JWT.
func (c *Client) Login(ctx context.Context, shortName, password string) error {
	body := map[string]string{
		"short_name": shortName,
		"password":   password,
	}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/spidipagos/login", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("spidi: login request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &SPIDIESpecificError{StatusCode: resp.StatusCode, Raw: string(raw)}
	}

	var tr TokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return fmt.Errorf("spidi: login decode: %w", err)
	}

	c.mu.Lock()
	c.token = tr.Token
	c.tokenExp = time.Now().Add(15 * time.Minute) // SPIDI no retorna exp, asumimos 15min
	c.mu.Unlock()

	return nil
}

// ensureToken verifica si el token necesita refresh.
func (c *Client) ensureToken(ctx context.Context, creds struct{ ShortName, Password string }) error {
	c.mu.RLock()
	expired := time.Now().After(c.tokenExp.Add(-tokenLeeway)) || c.token == ""
	c.mu.RUnlock()
	if expired {
		return c.Login(ctx, creds.ShortName, creds.Password)
	}
	return nil
}

// getToken retorna el token cacheado.
func (c *Client) getToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// DoRequest ejecuta un request genérico con el token incluido.
func (c *Client) DoRequest(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("spidi: request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return &SPIDIESpecificError{
			StatusCode: resp.StatusCode,
			Raw:        string(raw),
		}
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("spidi: decode: %w", err)
		}
	}
	return nil
}

// CreateAgreement llama al endpoint de agreements de SPIDI.
func (c *Client) CreateAgreement(ctx context.Context, title, bankAccountID string, split bool, paymentMethods map[string]bool) (*AgreementResponse, error) {
	body := map[string]interface{}{
		"title":                 title,
		"default_bank_account_id": bankAccountID,
		"split":                 split,
		"payment_methods":       paymentMethods,
	}
	var resp AgreementResponse
	if err := c.DoRequest(ctx, http.MethodPost, "/api/v1/ext/agreements", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateSession crea una sesión de pago (botón).
func (c *Client) CreateSession(ctx context.Context, agreementID string, amount float64, currency, orderID, description, successURL, failureURL string) (*SessionResponse, error) {
	body := map[string]interface{}{
		"agreement_id":       agreementID,
		"amount_reference":   amount,
		"currency_reference": currency,
		"identifier_label":   "Nro de orden",
		"identifier":         orderID,
		"description":        description,
		"success_url":        successURL,
		"failure_url":        failureURL,
		"duration_minutes":   5,
	}
	var resp SessionResponse
	if err := c.DoRequest(ctx, http.MethodPost, "/api/v1/ext/payment-sessions/buttons", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSessionStatus consulta el estado de una sesión.
func (c *Client) GetSessionStatus(ctx context.Context, sessionID string) (*SessionStatus, error) {
	var resp SessionStatus
	if err := c.DoRequest(ctx, http.MethodGet, "/api/v1/ext/payment-sessions/status/"+url.PathEscape(sessionID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
