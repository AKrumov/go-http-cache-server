package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

const basicAuthRealm = `Basic realm="go-gradle-cache"`

type authConfig struct {
	enabled  bool
	username string
	password string
}

func newAuthConfig(username, password string) (authConfig, error) {
	if username == "" && password == "" {
		return authConfig{}, nil
	}
	if username == "" || password == "" {
		return authConfig{}, errIncompleteAuthConfig
	}
	return authConfig{
		enabled:  true,
		username: username,
		password: password,
	}, nil
}

func (cs *CacheServer) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	if !cs.auth.enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || !cs.auth.matches(username, password) {
			w.Header().Set("WWW-Authenticate", basicAuthRealm)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (auth authConfig) matches(username, password string) bool {
	usernameHash := sha256.Sum256([]byte(username))
	expectedUsernameHash := sha256.Sum256([]byte(auth.username))
	passwordHash := sha256.Sum256([]byte(password))
	expectedPasswordHash := sha256.Sum256([]byte(auth.password))

	usernameMatches := subtle.ConstantTimeCompare(usernameHash[:], expectedUsernameHash[:]) == 1
	passwordMatches := subtle.ConstantTimeCompare(passwordHash[:], expectedPasswordHash[:]) == 1
	return usernameMatches && passwordMatches
}
