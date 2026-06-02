package server

import (
	"crypto/subtle"
	"net/http"
	"sync"
	"time"
)

const sessionCookie = "fc_session"

type Authenticator interface {
	Login(password string) bool
	Check(r *http.Request) bool
	Wrap(h http.HandlerFunc) http.HandlerFunc
}

type SessionAuth struct {
	password string
	mu       sync.RWMutex
	sessions map[string]time.Time
}

func NewSessionAuth(password string) Authenticator {
	if password == "" {
		return NoopAuth{}
	}
	return &SessionAuth{
		password: password,
		sessions: make(map[string]time.Time),
	}
}

type NoopAuth struct{}

func (NoopAuth) Login(string) bool           { return true }
func (NoopAuth) Check(*http.Request) bool    { return true }
func (NoopAuth) Wrap(h http.HandlerFunc) http.HandlerFunc { return h }

func (a *SessionAuth) Login(password string) bool {
	return subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) == 1
}

func (a *SessionAuth) Check(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	a.mu.RLock()
	_, ok := a.sessions[c.Value]
	a.mu.RUnlock()
	return ok
}

func (a *SessionAuth) Wrap(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Check(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func (a *SessionAuth) SetSession(token string) {
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(24 * time.Hour)
	a.mu.Unlock()
}

func (a *SessionAuth) ClearSession(token string) {
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}
