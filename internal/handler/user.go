package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/jguimeradev/priv-go-rest/internal/domain"
	"github.com/jguimeradev/priv-go-rest/internal/service"
)

/*
	"/", homePage
	"/health", health
	"GET /users", getUsers
	"POST /users", createUser
	"GET /users/{id}", getUser
	"PATCH /users/{id}", updateUser
	"DELETE /users/{id}", deleteUser
*/

type UserHandler struct {
	userSvc service.UserService
}

func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{
		userSvc: svc,
	}
}

func (u UserHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /users/{id}", u.HandleGetUser)
}

func (u UserHandler) HandleGetUser(w http.ResponseWriter, r *http.Request) {

	i := r.PathValue("id")

	id, err := strconv.Atoi(i)

	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	user, err := u.userSvc.ReadUser(id)

	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err = json.NewEncoder(w).Encode(user); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		return
	}
}
