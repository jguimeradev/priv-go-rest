package domain

import "errors"

type UpdateUserParams struct {
	Name  string
	Email string
}

type User struct {
	ID       int
	Name     string
	Email    string
	Password string
}

type UserResponse struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// a bridge between UpdateUserRequest and UpdateUserParams (handler / service )
type UpdateUserInput struct {
	Name  *string
	Email *string
}

var ErrUserNotFound = errors.New("user not found")
var ErrMailAlreadyExists = errors.New("email already in use")
var ErrSamePassword = errors.New("same password")
var ErrInvalidPassword = errors.New("invalid password")
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrInvalidToken = errors.New("invalid token")
