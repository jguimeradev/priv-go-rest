package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jguimeradev/priv-go-rest/internal/domain"
)

type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{
		db: db,
	}
}

func (d *UserRepo) Create(name string, email string, password string) (int, error) {

	res, err := d.db.Exec("INSERT INTO users (name, email, password) VALUES (?,?,?)", name, email, password)

	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()

	if err != nil {
		return 0, err
	}

	return int(id), nil
}

func (d *UserRepo) All() ([]domain.User, error) {

	rows, err := d.db.Query("SELECT id,name, email from users")

	var users []domain.User

	if err != nil {
		return []domain.User{}, err
	}

	defer rows.Close()

	for rows.Next() {
		var user domain.User
		if err := rows.Scan(&user.ID, &user.Name, &user.Email); err != nil {
			return []domain.User{}, err
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return []domain.User{}, err
	}

	return users, nil

}

func (d *UserRepo) Read(id int) (domain.User, error) {

	var user domain.User
	err := d.db.QueryRow("SELECT id, name, email, password from users WHERE id = ?", id).Scan(&user.ID, &user.Name, &user.Email, &user.Password)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("Read: %w", err)
	}

	return user, nil

}

func (d *UserRepo) Update(id int, params domain.UpdateUserParams) error {

	_, err := d.db.Exec("UPDATE users SET name = ?, email = ? WHERE id = ?", params.Name, params.Email, id)

	if err != nil {
		return err
	}

	return nil

}

func (d *UserRepo) Delete(id int) error {

	_, err := d.db.Exec("DELETE FROM users WHERE id = ?", id)

	if err != nil {
		return err
	}

	return nil
}

func (d *UserRepo) UpdatePassword(id int, password string) error {

	_, err := d.db.Exec("UPDATE users SET password = ? WHERE id = ?", password, id)

	if err != nil {
		return err
	}

	return nil
}
