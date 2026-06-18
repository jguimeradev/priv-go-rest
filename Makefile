.PHONY: run build clean lint list
all:	build
	./bin/app
build:	clean
	go build -o bin/app cmd/main.go
run:
	go run cmd/main.go
clean:
	rm -f ./bin/app
lint:
	go vet ./...
list:
	go list ./...