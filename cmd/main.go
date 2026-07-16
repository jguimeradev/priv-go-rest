package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-sql-driver/mysql"
	"github.com/jguimeradev/priv-go-rest/internal/config"
	"github.com/jguimeradev/priv-go-rest/internal/handler"
	"github.com/jguimeradev/priv-go-rest/internal/repository"
	"github.com/jguimeradev/priv-go-rest/internal/service"
)

func main() {

	//config
	c, err := config.Load()

	if len(err) > 0 {
		for _, e := range err {
			fmt.Println(e.Error())
		}
		os.Exit(1)
	}

	cfg := mysql.NewConfig()
	cfg.ClientFoundRows = true
	cfg.User = c.DbUser
	cfg.Passwd = c.DbPassword
	cfg.Net = c.NetProt
	cfg.Addr = c.DbHost + ":" + c.DbPort
	cfg.DBName = c.DbName

	//database

	connector, errdb := mysql.NewConnector(cfg)

	if errdb != nil {
		log.Fatal(errdb)
	}

	db := sql.OpenDB(connector)

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	//wiring

	userRepo := repository.NewUserRepo(db)

	authSvc := service.NewAuthSvc(userRepo, c.JwtSecret, c.TokenLifetime)

	userSvc := service.NewUserSvc(userRepo)

	userHandler := handler.NewUserHandler(userSvc)

	authHandler := handler.NewAuthHandler(authSvc)

	//server

	mux := http.NewServeMux()

	userHandler.RegisterRoutes(mux)
	authHandler.RegisterRoutes(mux)

	log.Fatal(http.ListenAndServe(":"+c.AppPort, mux))

}
