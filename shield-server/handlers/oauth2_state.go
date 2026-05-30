package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"sync"
	"time"
)

const (
	stateTTL             = 10 * time.Minute
	stateCleanupInterval = 2 * time.Minute
)

// StateEntry holds the PKCE code verifier and creation timestamp for an OAuth2 state parameter.
type StateEntry struct {
	CodeVerifier string
	CreatedAt    time.Time
}

// StateStore provides a thread-safe in-memory store for OAuth2 state parameters.
// Entries expire after stateTTL and are cleaned up periodically.
type StateStore struct {
	mu     sync.RWMutex
	states map[string]*StateEntry
}

// NewStateStore creates a new StateStore and starts a background cleanup goroutine.
func NewStateStore() *StateStore {
	s := &StateStore{
		states: make(map[string]*StateEntry),
	}
	go s.cleanupLoop()
	return s
}

// GenerateState creates a cryptographically random state string and its associated
// PKCE code_verifier, stores them, and returns (state, code_verifier, code_challenge).
func (s *StateStore) GenerateState() (state, codeVerifier, codeChallenge string, err error) {
	// Generate 32-byte random state
	stateBytes := make([]byte, 32)
	if _, err = rand.Read(stateBytes); err != nil {
		return
	}
	state = hex.EncodeToString(stateBytes)

	// Generate PKCE code_verifier (43-128 chars, RFC 7636)
	verifierBytes := make([]byte, 32)
	if _, err = rand.Read(verifierBytes); err != nil {
		return
	}
	codeVerifier = base64.RawURLEncoding.EncodeToString(verifierBytes)

	// code_challenge = BASE64URL(SHA256(code_verifier))
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge = base64.RawURLEncoding.EncodeToString(h[:])

	s.mu.Lock()
	s.states[state] = &StateEntry{
		CodeVerifier: codeVerifier,
		CreatedAt:    time.Now(),
	}
	s.mu.Unlock()

	return
}

// ValidateAndConsume checks if the state exists and has not expired.
// If valid, it removes the state (one-time use) and returns the associated code_verifier.
func (s *StateStore) ValidateAndConsume(state string) (codeVerifier string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.states[state]
	if !exists {
		return "", false
	}
	delete(s.states, state)

	if time.Since(entry.CreatedAt) > stateTTL {
		return "", false
	}

	return entry.CodeVerifier, true
}

// cleanupLoop periodically removes expired state entries.
func (s *StateStore) cleanupLoop() {
	ticker := time.NewTicker(stateCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.states {
			if now.Sub(v.CreatedAt) > stateTTL {
				delete(s.states, k)
			}
		}
		s.mu.Unlock()
	}
}
