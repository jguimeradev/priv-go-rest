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
	ID    int
	Name  string
	Email string
}

var ErrUserNotFound = errors.New("user not found")
var ErrUserAlreadyExists = errors.New("user already exists")
var ErrSamePassword = errors.New("same password")
var ErrInvalidPassword = errors.New("invalid password")
