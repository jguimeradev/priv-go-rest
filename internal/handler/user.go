package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-playground/validator/v10"
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

var validate *validator.Validate = validator.New(validator.WithRequiredStructEnabled())

type CreateUserRequest struct {
	Name     string `json:"name" validate:"required,min=5,max=100"`
	Email    string `json:"email" validate:"required,email,max=255"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

type UserHandler struct {
	userSvc service.UserService
}

func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{
		userSvc: svc,
	}
}

func (c *CreateUserRequest) validateRequest() error {

	if err := validate.Struct(c); err != nil {
		return err
	}
	return nil
}

func (u UserHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /users/{id}", u.HandleGetUser)
	mux.HandleFunc("POST /users", u.HandlePostUser)
	//mux.HandleFunc("PATCH /users/{id}", u.HandlePatchUser)
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

func (u UserHandler) HandlePostUser(w http.ResponseWriter, r *http.Request) {

	var c CreateUserRequest

	err := json.NewDecoder(r.Body).Decode(&c)

	if err != nil {
		http.Error(w, "Malformed Request syntax", http.StatusBadRequest)
		return
	}

	if err = c.validateRequest(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := u.userSvc.CreateUser(c.Name, c.Email, c.Password)

	if err != nil {
		if errors.Is(err, domain.ErrUserAlreadyExists) {
			http.Error(w, "User already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	ur := domain.UserResponse{
		ID:    id,
		Name:  c.Name,
		Email: c.Email,
	}

	sid := strconv.Itoa(id)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/users/"+sid)
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(ur)
	if err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		return
	}

}

func HandlePatchUser(w http.ResponseWriter, r *http.Request) {

	var c CreateUserRequest

	err := json.NewDecoder(r.Body).Decode(&c)

	if err != nil {
		http.Error(w, "Malformed Request syntax", http.StatusBadRequest)
		return
	}

}
