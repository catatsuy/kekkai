.PHONY: all
all: bin/kekkai

go.mod go.sum:
	go mod tidy

bin/kekkai: cmd/kekkai/main.go go.mod $(wildcard internal/**/*.go)
	go build -o bin/kekkai cmd/kekkai/main.go

.PHONY: test
test:
	go test -cover -count 1 ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: staticcheck
staticcheck:
	staticcheck -checks="all,-ST1000" ./...

.PHONY: clean
clean:
	rm -rf bin/*
