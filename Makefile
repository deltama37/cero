.PHONY: build test fmt fmt-check vet lint check clean run

BIN_DIR := bin
CEROC   := $(BIN_DIR)/ceroc

build:
	go build -o $(CEROC) ./cmd/ceroc

test:
	go test ./...

fmt:
	gofmt -l -w .

# fmt-check fails when gofmt would rewrite any file. CI uses this instead of fmt,
# which rewrites sources in place and would hide a formatting diff.
fmt-check:
	@files=$$(gofmt -l .); \
	status=$$?; \
	if [ $$status -ne 0 ]; then \
		exit $$status; \
	fi; \
	if [ -n "$$files" ]; then \
		echo "gofmt would reformat:"; \
		printf '%s\n' "$$files"; \
		exit 1; \
	fi

vet:
	go vet ./...

# check formats sources in place, then runs vet and tests.
# CI calls fmt-check, vet, and test so an unformatted tree fails the job.
check: fmt vet test

clean:
	rm -rf $(BIN_DIR)

run: build
	$(CEROC) $(ARGS)
