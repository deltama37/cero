.PHONY: build test fmt fmt-check vet lint check clean run selfhost selfhost-build fmt-cero fmt-cero-check

CERO_SOURCES = $(shell find examples std compiler -name '*.cero' | sort)

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

# selfhost checks that the Cero-written compiler reproduces itself (ADR-0015).
selfhost:
	./scripts/selfhost.sh

selfhost-build: build
	./bin/ceroc build compiler/main.cero -o bin/ceroc-cero.wasm

# fmt-cero formats every Cero source in the repository with the Cero-written compiler (ADR-0016).
fmt-cero: selfhost-build
	./scripts/ceroc-cero fmt -w $(CERO_SOURCES)

fmt-cero-check: selfhost-build
	./scripts/ceroc-cero fmt --check $(CERO_SOURCES)
