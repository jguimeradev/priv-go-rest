package main

import (
	"fmt"

	"github.com/jguimeradev/priv-go-rest/internal/config"
)

func main() {
	fmt.Println("Hello, World")
	c := config.Load()
	fmt.Println(c)
}
