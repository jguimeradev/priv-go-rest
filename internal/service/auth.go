package service

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jguimeradev/priv-go-rest/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type AuthService interface {
	Login(email string, password string) (string, error)
	VerifyToken(tokenString string) (int, error)
}

type AuthRepository interface {
	ReadByEmail(email string) (domain.User, error)
}

type AuthSvc struct {
	authRepo      AuthRepository
	jwtSecret     string
	tokenLifetime time.Duration
}

func NewAuthSvc(repo AuthRepository, jwtSecret string, tokenLifetime time.Duration) *AuthSvc {
	return &AuthSvc{
		authRepo:      repo,
		jwtSecret:     jwtSecret,
		tokenLifetime: tokenLifetime,
	}
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

	claims := jwt.RegisteredClaims{
		Subject:   strconv.Itoa(u.ID),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.tokenLifetime)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(a.jwtSecret))

	if err != nil {
		return "", fmt.Errorf("Login: %w", err)
	}

	return tokenString, nil

}

func (a *AuthSvc) VerifyToken(tokenString string) (int, error) {

	claims := &jwt.RegisteredClaims{}

	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		return []byte(a.jwtSecret), nil
	})

	if err != nil {
		return 0, domain.ErrInvalidToken
	}

	id, err := claims.GetSubject()

	if err != nil {
		return 0, fmt.Errorf("VerifyToken: %w", err)
	}

	i, err := strconv.Atoi(id)

	if err != nil {
		return 0, fmt.Errorf("VerifyToken: %w", err)
	}

	return i, nil

}
