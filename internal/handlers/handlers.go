// handlers.go - Handlers HTTP para la API REST.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"spidi-agent/internal/models"
	"spidi-agent/internal/service"
)

// Handler maneja las rutas HTTP.
type Handler struct {
	svc    *service.Service
	logger *slog.Logger
}

// NewHandler crea un nuevo handler.
func NewHandler(svc *service.Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// RegisterRoutes registra las rutas en el mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/clientes", h.handleClientes)
	mux.HandleFunc("/pagos", h.handlePagos)
	mux.HandleFunc("/pagos/", h.handlePagoByID)
	mux.HandleFunc("/webhook", h.handleWebhook)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, models.ErrorResponse{Success: false, Message: "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, models.SuccessResponse{Success: true, Data: map[string]string{"status": "healthy"}})
}

// handleClientes maneja POST /clientes y GET /clientes.
func (h *Handler) handleClientes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createTenant(w, r)
	case http.MethodGet:
		h.listTenants(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, models.ErrorResponse{Success: false, Message: "method not allowed"})
	}
}

func (h *Handler) createTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		Password        string `json:"password"`
		BankAccountID   string `json:"bank_account_id"`
		Environment     string `json:"environment"`
		CallbackBaseURL string `json:"callback_base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Success: false, Message: "invalid JSON"})
		return
	}

	writeJSON(w, http.StatusCreated, models.SuccessResponse{
		Success: true,
		Data: map[string]string{
			"tenant_id":   "generated",
			"short_name":  req.ShortName,
			"environment": req.Environment,
		},
	})
}

func (h *Handler) listTenants(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, models.SuccessResponse{Success: true, Data: []interface{}{}})
}

// handlePagos maneja POST /pagos (crear) y GET /pagos (listar).
func (h *Handler) handlePagos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createPayment(w, r)
	case http.MethodGet:
		h.listPayments(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, models.ErrorResponse{Success: false, Message: "method not allowed"})
	}
}

func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var req models.PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Success: false, Message: "invalid JSON"})
		return
	}
	if req.TenantID == "" || req.Amount <= 0 || req.Currency == "" || req.OrderID == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{
			Success: false,
			Message: "tenant_id, monto, moneda y orden son requeridos",
			Errors: map[string]string{
				"tenant_id": req.TenantID,
				"monto":     "debe ser mayor a 0",
				"moneda":    req.Currency,
				"orden":     req.OrderID,
			},
		})
		return
	}

	session, err := h.svc.CreatePayment(r.Context(), &req)
	if err != nil {
		h.logger.Error("create payment failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Success: false, Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, models.SuccessResponse{Success: true, Data: session})
}

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	// TODO: implementar listado desde BD
	writeJSON(w, http.StatusOK, models.SuccessResponse{Success: true, Data: []interface{}{}})
}

// handlePagoByID maneja GET /pagos/:id y DELETE /pagos/:id.
func (h *Handler) handlePagoByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/pagos/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Success: false, Message: "session ID requerido"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getPaymentStatus(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, models.ErrorResponse{Success: false, Message: "method not allowed"})
	}
}

func (h *Handler) getPaymentStatus(w http.ResponseWriter, r *http.Request, sessionID string) {
	status, err := h.svc.CheckStatus(r.Context(), sessionID)
	if err != nil {
		h.logger.Error("check status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, models.SuccessResponse{Success: true, Data: status})
}

// handleWebhook maneja POST /webhook.
func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, models.ErrorResponse{Success: false, Message: "method not allowed"})
		return
	}

	var event models.WebhookEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Success: false, Message: "invalid JSON"})
		return
	}

	if err := h.svc.HandleWebhook(r.Context(), &event); err != nil {
		h.logger.Error("webhook failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Success: false, Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, models.SuccessResponse{Success: true, Data: map[string]string{"received": "ok"}})
}

// writeJSON escribe una respuesta JSON.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
