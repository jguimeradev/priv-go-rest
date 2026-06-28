package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/go-sql-driver/mysql"
	"github.com/jguimeradev/priv-go-rest/internal/config"
	"github.com/jguimeradev/priv-go-rest/internal/repository"
)

func main() {

	c, err := config.Load()

	if len(err) > 0 {
		for _, e := range err {
			fmt.Println(e.Error())
		}
		os.Exit(1)
	}

	// Specify connection properties.
	cfg := mysql.NewConfig()
	cfg.User = c.DbUser
	cfg.Passwd = c.DbPassword
	cfg.Net = c.NetProt
	cfg.Addr = c.DbHost + ":" + c.DbPort
	cfg.DBName = c.DbName

	// Get a driver-specific connector.
	connector, errdb := mysql.NewConnector(cfg)

	if errdb != nil {
		log.Fatal(errdb)
	}

	// Get a database handle.
	db := sql.OpenDB(connector)

	// Confirm a successful connection.
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	u := repository.NewUserRepo(db)
	fmt.Println(u)

}
