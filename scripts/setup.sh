#!/bin/bash

# -e (exit on error) 
# -x (print every command) 

set -xe

#
# Examples:
# dirname /usr/bin/  -> "/usr"
# $BASH_SOURCE holds the path to the script being executed. 
# realpath gives the absolute path of current directory. 
# this command allows to go to the parent directory of the script even is it is
# executed from outside of its path.

cd $(dirname $(dirname $(realpath "$BASH_SOURCE")))

rm -f go.mod go.sum

mkdir -p cmd internal/{handler,service,repository,middleware,config}

go mod init github.com/jguimeradev/priv-go-rest
go get github.com/joho/godotenv
go mod tidy


cat > cmd/main.go <<EOF
package main

import "fmt"

func main(){
    fmt.Println("Hello, World")
}
EOF



echo "The project is scaffolded. Start to code."
