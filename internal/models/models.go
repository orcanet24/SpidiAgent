package models

import (
	"time"
)

// TenantConfig representa un cliente registrado con sus credenciales SPIDI.
type TenantConfig struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	ShortName   string    `json:"short_name" db:"short_name"` // short_name de SPIDI
	// Password cifrado en reposo (bcrypt)
	PasswordEncrypted string    `json:"-" db:"password_encrypted"`
	AgreementID       string    `json:"agreement_id,omitempty" db:"agreement_id"`
	Environment       string    `json:"environment" db:"environment"` // sandbox / production
	CallbackBaseURL   string    `json:"callback_base_url,omitempty" db:"callback_base_url"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// PaymentRequest es lo que tus sistemas envían al Agente.
type PaymentRequest struct {
	TenantID      string  `json:"tenant_id"`
	Amount        float64 `json:"monto"`
	Currency      string  `json:"moneda"` // USD, EUR, COP, USDT, VES
	OrderID       string  `json:"orden"`
	Description   string  `json:"descripcion,omitempty"`
	SuccessURL    string  `json:"url_exito,omitempty"`
	FailureURL    string  `json:"url_fallo,omitempty"`
	CallbackURL   string  `json:"callback_url,omitempty"` // override del tenant
}

// PaymentSession es la respuesta al crear un pago.
type PaymentSession struct {
	SessionID     string    `json:"session_id"`
	TenantID      string    `json:"tenant_id"`
	OrderID       string    `json:"orden"`
	PaymentURL    string    `json:"payment_url"`
	Currency      string    `json:"moneda"`
	Amount        float64   `json:"monto"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time `json:"creado_en"`
}

// PaymentStatus representa el estado actual de una sesión de pago.
type PaymentStatus struct {
	SessionID       string    `json:"session_id"`
	Status          string    `json:"status"` // pending, paid, expired, failed
	PaymentMethod   string    `json:"payment_method,omitempty"`
	TransactionID   string    `json:"transaction_id,omitempty"`
	TransactionURL  string    `json:"transaction_url,omitempty"`
	AmountVES       float64   `json:"amount_ves,omitempty"`
	PaidAt          time.Time `json:"paid_at,omitempty"`
	RawResponse     string    `json:"raw_response,omitempty"`
}

// OperationLog para auditoría de cada operación realizada.
type OperationLog struct {
	ID          string    `json:"id" db:"id"`
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	Operation   string    `json:"operation" db:"operation"` // create_payment, check_status, etc.
	ResourceID  string    `json:"resource_id,omitempty" db:"resource_id"`
	Status      string    `json:"status" db:"status"` // success, error
	RequestJSON string    `json:"request_json" db:"request_json"`
	ResponseJSON string   `json:"response_json" db:"response_json"`
	ErrorMessage string   `json:"error_message,omitempty" db:"error_message"`
	DurationMS  int       `json:"duration_ms" db:"duration_ms"`
	ClientIP    string    `json:"client_ip,omitempty" db:"client_ip"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// WebhookEvent representa un evento recibido de SPIDI.
type WebhookEvent struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"` // payment.paid, payment.expired, etc.
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id,omitempty"`
	Timestamp string `json:"timestamp"`
	RawPayload string `json:"-"`
}

// ErrorResponse estandarizada.
type ErrorResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Errors  map[string]string `json:"errors,omitempty"`
}

// SuccessResponse estandarizada.
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
}
