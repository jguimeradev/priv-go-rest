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

type UpdateUserRequest struct {
	Name  *string `json:"name" validate:"omitempty,min=5,max=100"`
	Email *string `json:"email" validate:"omitempty,email,max=255"`
}

type UserHandler struct {
	userSvc service.UserService
}

func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{
		userSvc: svc,
	}
}

func (u UserHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /users", u.HandleGetUsers)
	mux.HandleFunc("GET /users/{id}", u.HandleGetUser)
	mux.HandleFunc("POST /users", u.HandlePostUser)
	mux.HandleFunc("PATCH /users/{id}", u.HandlePatchUser)
	mux.HandleFunc("DELETE /users/{id}", u.HandleDeleteUser)
}

func (c *CreateUserRequest) validateRequest() error {

	if err := validate.Struct(c); err != nil {
		return err
	}
	return nil
}

func (u *UpdateUserRequest) validateRequest() error {

	if err := validate.Struct(u); err != nil {
		return err
	}
	return nil
}

func (u UserHandler) HandleGetUsers(w http.ResponseWriter, r *http.Request) {

	users, err := u.userSvc.FetchAllUsers()

	if err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(users); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		return
	}

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
		if errors.Is(err, domain.ErrMailAlreadyExists) {
			http.Error(w, "Mail already exists", http.StatusConflict)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(ur)
	if err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		return
	}

}

func (u UserHandler) HandlePatchUser(w http.ResponseWriter, r *http.Request) {

	i := r.PathValue("id")

	id, err := strconv.Atoi(i)

	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest) //400 parse
		return
	}

	var p UpdateUserRequest

	err = json.NewDecoder(r.Body).Decode(&p)

	if err != nil {
		http.Error(w, "Malformed Request syntax", http.StatusBadRequest) //400 decode
		return
	}

	if err = p.validateRequest(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest) //400 validation
		return
	}

	input := domain.UpdateUserInput{
		Name:  p.Name,
		Email: p.Email,
	}

	response, err := u.userSvc.UpdateUser(id, input) //404, 409, 500

	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, domain.ErrMailAlreadyExists) {
			http.Error(w, "Email already in use", http.StatusConflict)
			return
		}
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		return
	}
}

func (u UserHandler) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {

	i := r.PathValue("id")

	id, err := strconv.Atoi(i)

	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	err = u.userSvc.DeleteUser(id)

	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent) //204

}
