// store.go - CRUD de tenants en SQLite con cifrado.
package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"spidi-agent/internal/crypto"
	"spidi-agent/internal/database"
	"spidi-agent/internal/models"
)

var (
	ErrTenantNotFound = errors.New("tenant: not found")
	ErrTenantExists   = errors.New("tenant: already exists")
)

// Store gestiona tenants en SQLite.
type Store struct {
	db        *database.DB
	masterKey string // para cifrar contraseñas
	logger    *slog.Logger
}

// NewStore crea un nuevo store de tenants.
func NewStore(db *database.DB, masterKey string, logger *slog.Logger) *Store {
	return &Store{db: db, masterKey: masterKey, logger: logger}
}

// RegisterTenant registra un nuevo tenant, cifrando su contraseña.
func (s *Store) RegisterTenant(ctx context.Context, name, shortName, password, bankAccountID, environment, callbackURL string) (*models.TenantConfig, error) {
	if err := validateTenant(name, shortName, password); err != nil {
		return nil, err
	}

	encryptedPass, err := crypto.Encrypt(password, s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("tenant: encrypt: %w", err)
	}

	id, _ := crypto.GenerateRandomBytes(16)
	now := time.Now().UTC()
	tenant := &models.TenantConfig{
		ID:                strings.ReplaceAll(string(id), "-", ""),
		Name:              name,
		ShortName:         shortName,
		PasswordEncrypted: encryptedPass,
		AgreementID:       bankAccountID, // se asigna después de crear agreement
		Environment:       environment,
		CallbackBaseURL:   callbackURL,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	err = s.db.Write(func(tx *sql.DB) error {
		_, err := tx.Exec(`INSERT INTO tenants (id, name, short_name, password_encrypted, agreement_id, environment, callback_base_url, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tenant.ID, tenant.Name, tenant.ShortName, tenant.PasswordEncrypted,
			tenant.AgreementID, tenant.Environment, tenant.CallbackBaseURL,
			tenant.CreatedAt, tenant.UpdatedAt)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("tenant: insert: %w", err)
	}

	return tenant, nil
}

// GetTenant retiene un tenant por su ID o short_name.
func (s *Store) GetTenant(ctx context.Context, key string) (*models.TenantConfig, error) {
	var row *sql.Row
	if len(key) == 36 { // UUID v4
		row = s.db.QueryRow(`SELECT id, name, short_name, password_encrypted, agreement_id, environment, callback_base_url, created_at, updated_at
			FROM tenants WHERE id = ? OR short_name = ?`, key, key)
	} else {
		row = s.db.QueryRow(`SELECT id, name, short_name, password_encrypted, agreement_id, environment, callback_base_url, created_at, updated_at
			FROM tenants WHERE id = ? OR short_name = ?`, key, key)
	}

	var t models.TenantConfig
	err := row.Scan(&t.ID, &t.Name, &t.ShortName, &t.PasswordEncrypted, &t.AgreementID, &t.Environment, &t.CallbackBaseURL, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantNotFound
		}
		return nil, fmt.Errorf("tenant: scan: %w", err)
	}
	return &t, nil
}

// VerifyPassword verifica contraseña contra hash bcrypt almacenado.
func (s *Store) VerifyPassword(ctx context.Context, tenantID, password string) error {
	tenant, err := s.GetTenant(ctx, tenantID)
	if err != nil {
		return ErrTenantNotFound
	}
	// Primero intentamos bcrypt directo, si no funciona, desciframos el AES y comparamos
	if crypto.VerifyPassword(tenant.PasswordEncrypted, password) {
		return nil
	}
	decrypted, err := crypto.Decrypt(tenant.PasswordEncrypted, s.masterKey)
	if err != nil {
		return errors.New("tenant: invalid password")
	}
	if crypto.ConstantTimeCompare(decrypted, password) {
		return nil
	}
	return errors.New("tenant: invalid password")
}

// UpdateAgreement actualiza el agreement ID de un tenant.
func (s *Store) UpdateAgreement(ctx context.Context, tenantID, agreementID string) error {
	return s.db.Write(func(tx *sql.DB) error {
		_, err := tx.Exec(`UPDATE tenants SET agreement_id = ?, updated_at = ? WHERE id = ?`,
			agreementID, time.Now().UTC(), tenantID)
		return err
	})
}

// ListTenants lista todos los tenants.
func (s *Store) ListTenants(ctx context.Context) ([]models.TenantConfig, error) {
	rows, err := s.db.Query(`SELECT id, name, short_name, password_encrypted, agreement_id, environment, callback_base_url, created_at, updated_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []models.TenantConfig
	for rows.Next() {
		var t models.TenantConfig
		err := rows.Scan(&t.ID, &t.Name, &t.ShortName, &t.PasswordEncrypted, &t.AgreementID, &t.Environment, &t.CallbackBaseURL, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

// DeleteTenant elimina un tenant.
func (s *Store) DeleteTenant(ctx context.Context, tenantID string) error {
	return s.db.Write(func(tx *sql.DB) error {
		_, err := tx.Exec(`DELETE FROM tenants WHERE id = ?`, tenantID)
		return err
	})
}

func validateTenant(name, shortName, password string) error {
	if name == "" || shortName == "" || password == "" {
		return errors.New("tenant: name, short_name, and password are required")
	}
	if len(password) < 8 {
		return errors.New("tenant: password must be at least 8 characters")
	}
	return nil
}
