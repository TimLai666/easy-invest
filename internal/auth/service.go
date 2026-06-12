package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SessionCookieName = "ei_session"

var (
	ErrRegistrationDisabled = errors.New("registration disabled")
	ErrInvalidCredential    = errors.New("invalid credential")
	ErrForbidden            = errors.New("forbidden")
)

type Service struct {
	db                 *pgxpool.Pool
	secret             []byte
	enableRegistration bool
}

type User struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

type APIKey struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	KeyPrefix   string     `json:"key_prefix"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type CreatedAPIKey struct {
	APIKey
	Plaintext string `json:"plaintext"`
}

func NewService(db *pgxpool.Pool, secret []byte, enableRegistration bool) *Service {
	return &Service{db: db, secret: secret, enableRegistration: enableRegistration}
}

func (s *Service) Register(ctx context.Context, email, password, displayName string) (User, error) {
	if !s.enableRegistration {
		return User{}, ErrRegistrationDisabled
	}
	if len(password) < 10 {
		return User{}, fmt.Errorf("password too short")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	var user User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id::text, email::text, display_name, created_at::text
	`, email, hash, displayName).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt)
	if err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_settings (user_id) VALUES ($1)`, user.ID); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	var user User
	var passwordHash string
	err := s.db.QueryRow(ctx, `
		SELECT id::text, email::text, display_name, created_at::text, password_hash
		FROM users
		WHERE email = $1 AND disabled_at IS NULL
	`, email).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrInvalidCredential
	}
	if err != nil {
		return User{}, "", err
	}
	if !VerifyPassword(password, passwordHash) {
		return User{}, "", ErrInvalidCredential
	}
	token := s.signSession(user.ID, time.Now().Add(7*24*time.Hour))
	return user, token, nil
}

func (s *Service) AuthenticateRequest(ctx context.Context, r *http.Request) (Principal, error) {
	if authz := r.Header.Get("Authorization"); strings.HasPrefix(authz, "Bearer ") {
		return s.authenticateAPIKey(ctx, strings.TrimSpace(strings.TrimPrefix(authz, "Bearer ")))
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err == nil && cookie.Value != "" {
		return s.authenticateSession(ctx, cookie.Value)
	}
	return Principal{}, ErrInvalidCredential
}

func (s *Service) authenticateSession(ctx context.Context, token string) (Principal, error) {
	userID, ok := s.verifySession(token)
	if !ok {
		return Principal{}, ErrInvalidCredential
	}
	var email string
	err := s.db.QueryRow(ctx, `SELECT email::text FROM users WHERE id = $1 AND disabled_at IS NULL`, userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrInvalidCredential
	}
	if err != nil {
		return Principal{}, err
	}
	return Principal{UserID: userID, Email: email, Via: "session"}, nil
}

func (s *Service) authenticateAPIKey(ctx context.Context, token string) (Principal, error) {
	hash := HashAPIKey(token)
	var principal Principal
	err := s.db.QueryRow(ctx, `
		SELECT k.id::text, k.user_id::text, u.email::text, k.scopes
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = $1
		  AND k.revoked_at IS NULL
		  AND (k.expires_at IS NULL OR k.expires_at > now())
		  AND u.disabled_at IS NULL
	`, hash).Scan(&principal.APIKeyID, &principal.UserID, &principal.Email, &principal.Scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrInvalidCredential
	}
	if err != nil {
		return Principal{}, err
	}
	principal.Via = "api_key"
	_, _ = s.db.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, principal.APIKeyID)
	return principal, nil
}

func (s *Service) CurrentUser(ctx context.Context, userID string) (User, error) {
	var user User
	err := s.db.QueryRow(ctx, `
		SELECT id::text, email::text, display_name, created_at::text
		FROM users
		WHERE id = $1 AND disabled_at IS NULL
	`, userID).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt)
	return user, err
}

func (s *Service) CreateAPIKey(ctx context.Context, principal Principal, name, description string, scopes []string, expiresAt *time.Time) (CreatedAPIKey, error) {
	if principal.Via != "session" {
		return CreatedAPIKey{}, ErrForbidden
	}
	plaintext, err := NewAPIKeyToken()
	if err != nil {
		return CreatedAPIKey{}, err
	}
	key := CreatedAPIKey{Plaintext: plaintext}
	err = s.db.QueryRow(ctx, `
		INSERT INTO api_keys (user_id, name, description, key_prefix, key_hash, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, name, description, key_prefix, scopes, created_at, expires_at, last_used_at, revoked_at
	`, principal.UserID, name, description, PrefixAPIKey(plaintext), HashAPIKey(plaintext), scopes, expiresAt).
		Scan(&key.ID, &key.Name, &key.Description, &key.KeyPrefix, &key.Scopes, &key.CreatedAt, &key.ExpiresAt, &key.LastUsedAt, &key.RevokedAt)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	_ = s.Audit(ctx, principal, "apikey.create", "api_keys", key.ID, map[string]any{"name": name, "scopes": scopes})
	return key, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, name, description, key_prefix, scopes, created_at, expires_at, last_used_at, revoked_at
		FROM api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []APIKey
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(&key.ID, &key.Name, &key.Description, &key.KeyPrefix, &key.Scopes, &key.CreatedAt, &key.ExpiresAt, &key.LastUsedAt, &key.RevokedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Service) RevokeAPIKey(ctx context.Context, principal Principal, id string) error {
	if principal.Via != "session" {
		return ErrForbidden
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE api_keys
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1 AND user_id = $2
	`, id, principal.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_ = s.Audit(ctx, principal, "apikey.revoke", "api_keys", id, nil)
	return nil
}

func (s *Service) RotateAPIKey(ctx context.Context, principal Principal, id string) (CreatedAPIKey, error) {
	if err := s.RevokeAPIKey(ctx, principal, id); err != nil {
		return CreatedAPIKey{}, err
	}
	var name, description string
	var scopes []string
	err := s.db.QueryRow(ctx, `SELECT name, description, scopes FROM api_keys WHERE id = $1 AND user_id = $2`, id, principal.UserID).Scan(&name, &description, &scopes)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	return s.CreateAPIKey(ctx, principal, name+" rotated", description, scopes, nil)
}

func (s *Service) Audit(ctx context.Context, principal Principal, action, entity, entityID string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO audit_log (user_id, api_key_id, action, entity, entity_id, detail)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, NULLIF($5, '')::uuid, $6)
	`, principal.UserID, principal.APIKeyID, action, entity, entityID, detail)
	return err
}

func (s *Service) signSession(userID string, expiresAt time.Time) string {
	payload := fmt.Sprintf("%s|%d", userID, expiresAt.Unix())
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	signature := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (s *Service) verifySession(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payloadBytes)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", false
	}
	payload := string(payloadBytes)
	parts = strings.Split(payload, "|")
	if len(parts) != 2 {
		return "", false
	}
	var unix int64
	if _, err := fmt.Sscanf(parts[1], "%d", &unix); err != nil {
		return "", false
	}
	if time.Now().After(time.Unix(unix, 0)) {
		return "", false
	}
	return parts[0], true
}

func NewAPIKeyToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "ei_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashAPIKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func PrefixAPIKey(token string) string {
	if len(token) <= 11 {
		return token
	}
	return token[:11]
}
