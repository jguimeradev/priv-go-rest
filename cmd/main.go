package main

import (
	"fmt"
	"os"

	"github.com/jguimeradev/priv-go-rest/internal/config"
)

func main() {

	c, err := config.Load()

	if len(err) > 0 {
		for _, e := range err {
			fmt.Println(e.Error())
		}
		os.Exit(1)
	}

	fmt.Println(c)

}
