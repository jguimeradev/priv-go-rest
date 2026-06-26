package main

import (
	"fmt"

	"github.com/jguimeradev/priv-go-rest/internal/config"
)

func main() {

	c := config.Load()
	fmt.Println(c)
}
