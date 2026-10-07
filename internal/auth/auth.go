package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"royalknight/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "auth_user"

type Service struct {
	db *storage.DB
}

func NewService(db *storage.DB) *Service {
	return &Service{db: db}
}

func (s *Service) HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func (s *Service) CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Service) Authenticate(username, password string) (*storage.Session, error) {
	user, err := s.db.GetUserByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if !s.CheckPassword(user.PasswordHash, password) {
		return nil, fmt.Errorf("invalid credentials")
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	if err := s.db.CreateSession(token, user.ID, expiresAt); err != nil {
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	return &storage.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) DestroySession(token string) error {
	return s.db.DeleteSession(token)
}

// Middleware verifies authentication token via Cookie or Bearer header.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string

		cookie, err := r.Cookie("session_token")
		if err == nil && cookie.Value != "" {
			token = cookie.Value
		} else {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if token == "" {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}

		user, err := s.db.ValidateSession(token)
		if err != nil || user == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, `{"error":"session expired or invalid"}`, http.StatusUnauthorized)
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
