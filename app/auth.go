package main

import (
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
	usernameBytes := []byte(username)
	expectedUsernameBytes := []byte(auth.username)
	passwordBytes := []byte(password)
	expectedPasswordBytes := []byte(auth.password)

	if len(usernameBytes) != len(expectedUsernameBytes) {
		return false
	}
	if len(passwordBytes) != len(expectedPasswordBytes) {
		return false
	}

	usernameMatches := subtle.ConstantTimeCompare(usernameBytes, expectedUsernameBytes) == 1
	passwordMatches := subtle.ConstantTimeCompare(passwordBytes, expectedPasswordBytes) == 1
	return usernameMatches && passwordMatches
}
