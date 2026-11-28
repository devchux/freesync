package managed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/devchux/freesync/internal/database"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "freesync_session"
const secureSessionCookie = "__Host-freesync_session"
const sessionLifetime = 7 * 24 * time.Hour

var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("unused login timing placeholder"), bcrypt.DefaultCost)

type Account struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"fullName"`
}

type credentials struct {
	Email    string `json:"email"`
	FullName string `json:"fullName"`
	Password string `json:"password"`
}

func emailAddress(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	return value, err == nil && address.Address == value && len(value) <= 254
}

func (s *Server) account(r *http.Request) (Account, error) {
	token := requestToken(r)
	if token == "" {
		return Account{}, sql.ErrNoRows
	}
	row, err := s.queries().ManagedAccountBySession(r.Context(), digest(token), time.Now().Unix())
	return publicAccount(row), err
}

func requestToken(r *http.Request) string {
	if value := r.Header.Get("Authorization"); value != "" {
		token, _ := strings.CutPrefix(value, "Bearer ")
		if !strings.HasPrefix(value, "Bearer ") {
			return ""
		}
		return token
	}
	name, _ := sessionCookieSettings(r)
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func loopbackHost(host string) bool {
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func sessionCookieSettings(r *http.Request) (string, bool) {
	if !loopbackHost(r.Host) || r.TLS != nil {
		return secureSessionCookie, true
	}
	return sessionCookie, false
}

func setSession(w http.ResponseWriter, r *http.Request, token string, age int) {
	name, secure := sessionCookieSettings(r)
	http.SetCookie(w, &http.Cookie{Name: name, Value: token, Path: "/", MaxAge: age, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}

func sameOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		site := r.Header.Get("Sec-Fetch-Site")
		return site == "" || site == "same-origin" || site == "none"
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host) && (u.Scheme == "https" || (u.Scheme == "http" && loopbackHost(r.Host)))
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// HandleAuth handles account and session operations.
func (s *Server) HandleAuth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.PathValue("action") {
	case "me":
		if r.Method != http.MethodGet {
			break
		}
		account, err := s.account(r)
		if err != nil {
			s.accountError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, account)
		return
	case "logout":
		if r.Method != http.MethodPost {
			break
		}
		if err := s.queries().DeleteManagedSession(ctx, digest(requestToken(r))); err != nil {
			s.respondError(w, err)
			return
		}
		setSession(w, r, "", -1)
		w.WriteHeader(http.StatusNoContent)
		return
	case "signup", "login":
		if r.Method != http.MethodPost {
			break
		}
		s.authenticate(w, r)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) allowPasswordAttempt() bool {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	now := time.Now()
	if now.Sub(s.authWindow) >= time.Second {
		s.authWindow, s.authAttempts = now, 0
	}
	if s.authAttempts >= 12 {
		return false
	}
	s.authAttempts++
	return true
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	var input credentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	email, valid := emailAddress(input.Email)
	signup := r.PathValue("action") == "signup"
	name := strings.TrimSpace(input.FullName)
	if !valid || len(input.Password) > 72 || (signup && (len(input.Password) < 12 || name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsRune(name, 0))) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !s.allowPasswordAttempt() {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	ctx := r.Context()
	if signup {
		err := s.signup(ctx, email, name, input.Password)
		if err != nil {
			s.respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		return
	}
	row, err := s.queries().ManagedAccountByEmail(ctx, email)
	account := publicAccount(row)
	hash := row.PasswordHash
	if errors.Is(err, sql.ErrNoRows) {
		hash = string(dummyPasswordHash)
	} else if err != nil {
		s.respondError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil || account.ID == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	token := secret()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if err := s.queries().DeleteExpiredManagedSessions(ctx, now.Unix()); err != nil {
		s.respondError(w, err)
		return
	}
	if err := s.queries().InsertManagedSession(ctx, digest(token), account.ID, now.Add(sessionLifetime).Unix()); err != nil {
		s.respondError(w, err)
		return
	}
	setSession(w, r, token, int(sessionLifetime.Seconds()))
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) signup(ctx context.Context, email, name, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	queries := s.queries()
	_, err = queries.ManagedAccountByEmail(ctx, email)
	if err == nil {
		return fs.ErrExist
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return queries.InsertManagedAccount(ctx, secret(), email, name, string(hash))
}

func (s *Server) accountError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.respondError(w, err)
}

func publicAccount(row database.ManagedAccount) Account {
	return Account{ID: row.ID, Email: row.Email, FullName: row.FullName}
}
