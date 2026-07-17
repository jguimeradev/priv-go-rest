package middleware

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const userIDKey ctxKey = 0

type TokenVerifier interface {
	VerifyToken(tokenString string) (int, error)
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func Auth(next http.Handler, t TokenVerifier) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		header := r.Header.Get("Authorization")
		if header == "" {
			unauthorized(w)
			return
		}

		token, found := strings.CutPrefix(header, "Bearer ")
		if !found {
			unauthorized(w)
			return
		}

		id, err := t.VerifyToken(token)

		if err != nil {
			unauthorized(w)
			return
		}

		c := context.WithValue(r.Context(), userIDKey, id)

		r = r.WithContext(c)

		next.ServeHTTP(w, r)

	})
}
