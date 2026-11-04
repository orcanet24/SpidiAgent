// service.go - Servicio principal que integra SPIDI + SQLite.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"spidi-agent/internal/database"
	"spidi-agent/internal/models"
	"spidi-agent/internal/queue"
	"spidi-agent/internal/spidi"
	"spidi-agent/internal/tenant"
)

// Service integra todos los componentes.
type Service struct {
	db          *database.DB
	tenantStore *tenant.Store
	spidiClient *spidi.Client
	queue       *queue.RequestQueue
	logger      *slog.Logger
}

// Config del servicio.
type Config struct {
	DB          *database.DB
	TenantStore *tenant.Store
	SpidiClient *spidi.Client
	Logger      *slog.Logger
}

// New crea el servicio principal.
func New(cfg Config) *Service {
	return &Service{
		db:          cfg.DB,
		tenantStore: cfg.TenantStore,
		spidiClient: cfg.SpidiClient,
		queue:       queue.NewRequestQueue(10, 3), // 10 req/sec, 3 retries
		logger:      cfg.Logger,
	}
}

// CreatePayment crea un pago y lo registra en la BD.
func (s *Service) CreatePayment(ctx context.Context, req *models.PaymentRequest) (*models.PaymentSession, error) {
	tenant, err := s.tenantStore.GetTenant(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}

	callbackURL := tenant.CallbackBaseURL
	if req.CallbackURL != "" {
		callbackURL = req.CallbackURL
	}
	if callbackURL != "" {
		callbackURL += "/webhook"
	}

	var spidiResp *spidi.SessionResponse
	err = s.queue.Do(ctx, func() error {
		var e error
		spidiResp, e = s.spidiClient.CreateSession(ctx,
			tenant.AgreementID, req.Amount, req.Currency,
			req.OrderID, req.Description,
			req.SuccessURL, req.FailureURL,
		)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("payment: spidi: %w", err)
	}

	session := &models.PaymentSession{
		SessionID:  spidiResp.Data.SessionID,
		TenantID:   tenant.ID,
		OrderID:    req.OrderID,
		PaymentURL: spidiResp.Data.PaymentURL,
		Currency:   req.Currency,
		Amount:     req.Amount,
		Status:     "pending",
		CreatedAt:  time.Now().UTC(),
	}

	err = s.db.Write(func(tx *sql.DB) error {
		_, err := tx.Exec(`INSERT INTO sessions (id, tenant_id, order_id, payment_url, currency, amount, status, spidi_session_id, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			session.SessionID, session.TenantID, session.OrderID,
			session.PaymentURL, session.Currency, session.Amount,
			"pending", session.SessionID, time.Now().UTC().Add(5*time.Minute))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("payment: db: %w", err)
	}

	s.logOperation(ctx, tenant.ID, "create_payment", session.SessionID, req, session, "")
	return session, nil
}

// CheckStatus consulta el estado de un pago.
func (s *Service) CheckStatus(ctx context.Context, sessionID string) (*models.PaymentStatus, error) {
	session, err := s.getSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	var statusResp *spidi.SessionStatus
	err = s.queue.Do(ctx, func() error {
		var e error
		statusResp, e = s.spidiClient.GetSessionStatus(ctx, session.SessionID)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("status: spidi: %w", err)
	}

	if statusResp.Data.Status != session.Status {
		_ = s.db.Write(func(tx *sql.DB) error {
			_, err := tx.Exec(`UPDATE sessions SET status = ? WHERE id = ?`,
				statusResp.Data.Status, session.SessionID)
			return err
		})
	}

	return &models.PaymentStatus{
		SessionID:      session.SessionID,
		Status:         statusResp.Data.Status,
		PaymentMethod:  statusResp.Data.PaymentMethod,
		TransactionID:  statusResp.Data.TransactionID,
		TransactionURL: statusResp.Data.TransactionURL,
		AmountVES:      statusResp.Data.Amount,
	}, nil
}

// HandleWebhook procesa un webhook recibido de SPIDI.
func (s *Service) HandleWebhook(ctx context.Context, event *models.WebhookEvent) error {
	return s.db.Write(func(tx *sql.DB) error {
		_, err := tx.Exec(`UPDATE sessions SET status = ? WHERE id = ?`,
			event.EventType, event.SessionID)
		return err
	})
}

// logOperation registra una operación en el log.
func (s *Service) logOperation(ctx context.Context, tenantID, op, resourceID string, req, resp interface{}, errMsg string) {
	reqJSON, _ := json.Marshal(req)
	respJSON, _ := json.Marshal(resp)
	_ = s.db.Write(func(tx *sql.DB) error {
		_, err := tx.Exec(`INSERT INTO operation_log (tenant_id, operation, resource_id, status, request_json, response_json, error_message)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			tenantID, op, resourceID, map[bool]string{true: "error", false: "success"}[errMsg != ""],
			string(reqJSON), string(respJSON), errMsg)
		return err
	})
}

// getSession obtiene una sesión de la BD.
func (s *Service) getSession(ctx context.Context, sessionID string) (*models.PaymentSession, error) {
	var session models.PaymentSession
	err := s.db.QueryRow(`SELECT id, tenant_id, order_id, payment_url, currency, amount, status, created_at
		FROM sessions WHERE id = ?`, sessionID).
		Scan(&session.SessionID, &session.TenantID, &session.OrderID,
			&session.PaymentURL, &session.Currency, &session.Amount,
			&session.Status, &session.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("session: not found: %w", err)
	}
	return &session, nil
}

// GetDB retiene la BD.
func (s *Service) GetDB() *database.DB {
	return s.db
}
