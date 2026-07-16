package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/jguimeradev/priv-go-rest/internal/domain"
	"github.com/jguimeradev/priv-go-rest/internal/service"
)

type AuthHandler struct {
	authSvc service.AuthService
}

func NewAuthHandler(svc service.AuthService) *AuthHandler {
	return &AuthHandler{
		authSvc: svc,
	}
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type AuthResponse struct {
	Token string `json:"token"`
}

func (a AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/login", a.HandlePostLogin)
}

func (a AuthHandler) HandlePostLogin(w http.ResponseWriter, r *http.Request) {

	var l LoginRequest

	err := json.NewDecoder(r.Body).Decode(&l)

	if err != nil {
		http.Error(w, "Malformed Request syntax", http.StatusBadRequest)
		return
	}

	if err = l.validateRequest(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	token, err := a.authSvc.Login(l.Email, l.Password)

	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	t := AuthResponse{
		Token: token,
	}

	err = json.NewEncoder(w).Encode(t)
	if err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		return
	}

}

func (l *LoginRequest) validateRequest() error {

	if err := validate.Struct(l); err != nil {
		return err
	}
	return nil
}
