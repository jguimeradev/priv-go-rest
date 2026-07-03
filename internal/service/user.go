package service

import (
	"errors"
	"fmt"

	"github.com/jguimeradev/priv-go-rest/internal/domain"
	"golang.org/x/crypto/bcrypt"
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

func NewUserResponse(u *domain.User) domain.UserResponse {

	r := domain.UserResponse{
		ID:    u.ID,
		Name:  u.Name,
		Email: u.Email,
	}

	return r
}

func hashPassword(password string) (string, error) {

	pwd, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	if err != nil {
		return "", err
	}

	return string(pwd), nil
}

func (s *UserSvc) ReadUser(id int) (domain.UserResponse, error) {

	u, err := s.userRepo.Read(id)

	if err != nil {
		return domain.UserResponse{}, err
	}

	ur := NewUserResponse(&u)

	return ur, nil
}

func (s *UserSvc) FetchAllUsers() ([]domain.UserResponse, error) {

	users, err := s.userRepo.All()

	if err != nil {
		return []domain.UserResponse{}, err
	}

	ur := []domain.UserResponse{}

	for _, user := range users {
		ur = append(ur, NewUserResponse(&user))
	}

	return ur, nil

}

func (s *UserSvc) CreateUser(name string, email string, password string) (int, error) {

	pwd, err := hashPassword(password)

	if err != nil {
		return 0, err
	}

	id, err := s.userRepo.Create(name, email, pwd)

	if err != nil {
		return 0, err
	}

	return id, nil
}

func (s *UserSvc) UpdateUser(id int, params domain.UpdateUserParams) error {

	err := s.userRepo.Update(id, params)

	if err != nil {
		return err
	}

	return nil
}

func (s *UserSvc) DeleteUser(id int) error {

	err := s.userRepo.Delete(id)

	if err != nil {
		return err
	}

	return nil
}
func (s *UserSvc) ChangePassword(id int, oldPassword string, newPassword string) error {

	u, err := s.userRepo.Read(id)

	if err != nil {
		return err
	}

	err = bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(oldPassword))

	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return domain.ErrInvalidPassword
		}
		return fmt.Errorf("ChangePassword: %w", err)
	}

	if oldPassword == newPassword {
		return domain.ErrSamePassword
	}

	newPwd, err := hashPassword(newPassword)

	if err != nil {
		return err
	}

	err = s.userRepo.UpdatePassword(id, newPwd)

	if err != nil {
		return err
	}

	return nil
}
