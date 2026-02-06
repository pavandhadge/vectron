# Common settings
BINARY_DIR := ./bin
GO_BUILD_FLAGS := -trimpath   # optional: cleaner builds
GOAMD64 ?= v3                 # v1=baseline, v2/v3/v4 enable newer CPU features (compat risk)
PGO_PROFILE ?=                # e.g. cpu.pprof (leave empty to disable PGO)
LD_FLAGS ?=                   # e.g. -s -w (smaller binaries, not faster)
AVX512_TAGS ?= avx512
CGO_FLAGS ?= CGO_ENABLED=1
PGO_FLAGS := $(if $(PGO_PROFILE),-pgo=$(PGO_PROFILE),)

.PHONY: all build clean windows linux test-e2e \
        build-placementdriver build-worker build-apigateway build-auth build-reranker

all: build windows

build: clean linux

linux: clean
	mkdir -p $(BINARY_DIR)
	$(CGO_FLAGS) GOOS=linux GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/placementdriver ./placementdriver/cmd/placementdriver
	$(CGO_FLAGS) GOOS=linux GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -tags $(AVX512_TAGS) -o $(BINARY_DIR)/worker        ./worker/cmd/worker
	$(CGO_FLAGS) GOOS=linux GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/apigateway   ./apigateway/cmd/apigateway
	$(CGO_FLAGS) GOOS=linux GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/authsvc      ./auth/service/cmd/auth
	$(CGO_FLAGS) GOOS=linux GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/reranker     ./reranker/cmd/reranker

windows: clean
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/placementdriver_windows.exe ./placementdriver/cmd/placementdriver
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/worker_windows.exe        ./worker/cmd/worker
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/apigateway_windows.exe   ./apigateway/cmd/apigateway
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/authsvc_windows.exe      ./auth/service/cmd/auth
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/reranker_windows.exe     ./reranker/cmd/reranker

# Per-component targets (Linux by default, no suffix)
build-placementdriver:
	mkdir -p $(BINARY_DIR)
	$(CGO_FLAGS) GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/placementdriver ./placementdriver/cmd/placementdriver

build-worker:
	mkdir -p $(BINARY_DIR)
	$(CGO_FLAGS) GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -tags $(AVX512_TAGS) -o $(BINARY_DIR)/worker ./worker/cmd/worker

build-apigateway:
	mkdir -p $(BINARY_DIR)
	$(CGO_FLAGS) GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/apigateway ./apigateway/cmd/apigateway

build-auth:
	mkdir -p $(BINARY_DIR)
	$(CGO_FLAGS) GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/authsvc ./auth/service/cmd/auth

build-reranker:
	mkdir -p $(BINARY_DIR)
	$(CGO_FLAGS) GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/reranker ./reranker/cmd/reranker

test-e2e:
	go test -v ./tests/e2e -run 'Test' -timeout 30m

clean:
	rm -rf $(BINARY_DIR)

# Optional shortcuts
build-all: linux windows

# Optional: build both platforms without cleaning in between
build-both:
	mkdir -p $(BINARY_DIR)
	$(CGO_FLAGS) GOOS=linux   GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/placementdriver          ./placementdriver/cmd/placementdriver
	$(CGO_FLAGS) GOOS=linux   GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -tags $(AVX512_TAGS) -o $(BINARY_DIR)/worker                   ./worker/cmd/worker
	$(CGO_FLAGS) GOOS=linux   GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/apigateway              ./apigateway/cmd/apigateway
	$(CGO_FLAGS) GOOS=linux   GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/authsvc                 ./auth/service/cmd/auth
	$(CGO_FLAGS) GOOS=linux   GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/reranker                ./reranker/cmd/reranker
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/placementdriver_windows.exe ./placementdriver/cmd/placementdriver
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/worker_windows.exe           ./worker/cmd/worker
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/apigateway_windows.exe      ./apigateway/cmd/apigateway
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/authsvc_windows.exe         ./auth/service/cmd/auth
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" -o $(BINARY_DIR)/reranker_windows.exe        ./reranker/cmd/reranker
