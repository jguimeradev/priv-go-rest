package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-playground/validator"
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

type CreateUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserHandler struct {
	userSvc service.UserService
}

func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{
		userSvc: svc,
	}
}

var validate *validator.Validate

func (c *CreateUserRequest) validateRequest() {

	validate = validator.New()
}

func (u UserHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /users/{id}", u.HandleGetUser)
	mux.HandleFunc("POST /users", u.HandlePostUser)
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

	c.validateRequest()

	id, err := u.userSvc.CreateUser(c.Name, c.Email, c.Password)

	//err: password, userexists, other errors: 500

	fmt.Println(id)

}
