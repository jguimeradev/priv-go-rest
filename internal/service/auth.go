package service

import (
	"time"

	"github.com/jguimeradev/priv-go-rest/internal/domain"
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
