package domain

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
