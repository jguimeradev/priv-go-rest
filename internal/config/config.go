package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	app_env        string = "APP_ENV"
	app_port       string = "APP_PORT"
	db_host        string = "DB_HOST"
	db_port        string = "DB_PORT"
	db_user        string = "DB_USER"
	db_password    string = "DB_PASSWORD"
	db_name        string = "DB_NAME"
	net_prot       string = "NET_PROT"
	jwt_secret     string = "JWT_SECRET"
	token_lifetime string = "TOKEN_LIFETIME"
)

type Config struct {
	AppEnv        string
	AppPort       string
	DbHost        string
	DbPort        string
	DbUser        string
	DbPassword    string
	DbName        string
	NetProt       string
	JwtSecret     string
	TokenLifetime time.Duration
}

func (c *Config) Validate() []error {

	msg := make([]error, 0)

	if c.AppEnv == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of APP_ENV is empty."))
	}
	if c.AppPort == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of APP_PORT is empty."))
	}
	if c.DbHost == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of DB_HOST is empty."))
	}
	if c.DbPort == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of DB_PORT is empty."))
	}
	if c.DbUser == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of DB_USER is empty."))
	}
	if c.DbPassword == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of DB_PASSWORD is empty."))
	}
	if c.DbName == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of DB_NAME is empty."))
	}
	if c.NetProt == "" {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of NET_PROT is empty."))
	}
	if c.JwtSecret == "" || len(c.JwtSecret) < 32 {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of JWT_SECRET is too short"))
	}
	if c.TokenLifetime == 0 {
		msg = append(msg, fmt.Errorf("[ERROR]: The value of TOKEN_LIFETIME is empty."))
	}

	return msg
}

func Load() (Config, []error) {

	s := os.Getenv(app_env)

	if len(s) == 0 {

		err := godotenv.Load(".env")

		if err != nil {
			return Config{}, []error{err}
		}

	}

	token_duration, err := time.ParseDuration(os.Getenv(token_lifetime))

	var token_lifetime_error error

	if err != nil {
		token_lifetime_error = fmt.Errorf("[ERROR]: TOKEN_LIFETIME is not a valid duration: %w", err)
	}

	c := Config{
		AppEnv:        os.Getenv(app_env),
		AppPort:       os.Getenv(app_port),
		DbHost:        os.Getenv(db_host),
		DbPort:        os.Getenv(db_port),
		DbUser:        os.Getenv(db_user),
		DbPassword:    os.Getenv(db_password),
		DbName:        os.Getenv(db_name),
		NetProt:       os.Getenv(net_prot),
		JwtSecret:     os.Getenv(jwt_secret),
		TokenLifetime: token_duration,
	}

	errMsg := c.Validate()

	if token_lifetime_error != nil {
		errMsg = append(errMsg, token_lifetime_error)
	}

	if len(errMsg) > 0 {
		return Config{}, errMsg
	}

	log.Println("[INFO]: Running in", c.AppEnv, "env")

	return c, nil

}
