package service

import (
	"github.com/jguimeradev/priv-go-rest/internal/domain"
)

type UserService interface {
	CreateUser(name string, email string, password string) (int, error)
	ReadUser(id int) (domain.UserResponse, error)
	FetchAllUsers() ([]domain.UserResponse, error)
	UpdateUser(id int, params domain.UpdateUserParams) error
	DeleteUser(id int) error
	ChangePassword(id int, oldPassword string, newPassword string) error
}

type UserRepository interface {
	Create(name string, email string, password string) (int, error)
	All() ([]domain.User, error)
	Read(id int) (domain.User, error)
	Update(id int, params domain.UpdateUserParams) error
	Delete(id int) error
	UpdatePassword(id int, password string) error
}

type UserSvc struct {
	userRepo UserRepository //to reach the database
}

func NewUserSvc(repo UserRepository) *UserSvc {
	return &UserSvc{
		userRepo: repo,
	}
}

func newUserResponse(u *domain.User) domain.UserResponse {

	r := domain.UserResponse{
		ID:    u.ID,
		Name:  u.Name,
		Email: u.Email,
	}

	return r
}

func (s *UserSvc) ReadUser(id int) (domain.UserResponse, error) {

	u, err := s.userRepo.Read(id)

	if err != nil {
		return domain.UserResponse{}, err
	}

	ur := newUserResponse(&u)

	return ur, nil
}
