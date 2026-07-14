package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/jguimeradev/priv-go-rest/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type AuthRepository interface {
	ReadByEmail(email string) (domain.User, error)
}

type AuthSvc struct {
	authRepo      AuthRepository
	jwtSecret     string
	tokenLifetime time.Duration
}

func NewAuthService(repo AuthRepository, jwtSecret string, tokenLifetime time.Duration) *AuthSvc {
	return &AuthSvc{
		authRepo:      repo,
		jwtSecret:     jwtSecret,
		tokenLifetime: tokenLifetime,
	}
}

type JWToken struct {
}

func (a *AuthSvc) Login(email string, password string) (string, error) {

	u, err := a.authRepo.ReadByEmail(email)

	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return "", domain.ErrInvalidCredentials
		}
		return "", fmt.Errorf("Login: %w", err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password))

	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", domain.ErrInvalidCredentials
		}
		return "", fmt.Errorf("Login: %w", err)
	}

	return u.Email, nil

}
