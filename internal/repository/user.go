package repository

import "database/sql"

type User struct {
	ID       int
	Name     string
	Email    string
	Password string
}

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

func (d *UserRepo) All() ([]User, error) {

	rows, err := d.db.Query("SELECT id,name,email, password from users")

	var users []User

	if err != nil {
		return []User{}, err
	}

	defer rows.Close()

	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.Password); err != nil {
			return []User{}, err
		}
		users = append(users, user)
	}

	return users, nil

}

func (d *UserRepo) Read() (User, error) {

}

func (d *UserRepo) Update() error {

}

func (d *UserRepo) Delete() error {

}
