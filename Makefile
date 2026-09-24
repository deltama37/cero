.PHONY: build test fmt vet lint check clean run

BIN_DIR := bin
CEROC   := $(BIN_DIR)/ceroc

build:
	go build -o $(CEROC) ./cmd/ceroc

test:
	go test ./...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

# check runs the same gates that CI should enforce: formatting, vet and tests.
check: fmt vet test

clean:
	rm -rf $(BIN_DIR)

run: build
	$(CEROC) $(ARGS)
