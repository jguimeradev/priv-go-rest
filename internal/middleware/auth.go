package middleware

import (
	"fmt"
	"net/http"
	"strings"
)

type TokenVerifier interface {
	VerifyToken(tokenString string) (int, error)
}

func unauthorized(w http.ResponseWriter) {
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func Auth(next http.Handler, t TokenVerifier) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		header := r.Header.Get("Authorization")
		if header == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
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

		fmt.Println(id)

		next.ServeHTTP(w, r) // Call the next handler
		// Post-processing logic
	})
}
