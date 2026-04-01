# ==============================
# COMMON SETTINGS (UNCHANGED)
# ==============================
BINARY_DIR := ./bin
GO_BUILD_FLAGS := -trimpath
GOAMD64 ?= v3
PGO_PROFILE ?=
LD_FLAGS ?=
AVX512_TAGS ?= avx512
CGO_FLAGS ?= CGO_ENABLED=1

PGO_FLAGS := $(if $(PGO_PROFILE),-pgo=$(PGO_PROFILE),)

# ==============================
# INTERNAL HELPERS (NEW - DRY)
# ==============================
define build_linux
	$(CGO_FLAGS) GOOS=linux GOARCH=amd64 GOAMD64=$(GOAMD64) \
	go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" $(1) -o $(2) $(3)
endef

define build_windows
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=$(GOAMD64) \
	go build $(GO_BUILD_FLAGS) $(PGO_FLAGS) -ldflags "$(LD_FLAGS)" $(1) -o $(2) $(3)
endef

# ==============================
# TARGETS
# ==============================
.PHONY: all build clean windows linux test-e2e \
        build-placementdriver build-worker build-apigateway build-auth build-reranker

all: build windows

build: clean linux

# ==============================
# LINUX BUILD (PARALLEL)
# ==============================
linux: clean
	mkdir -p $(BINARY_DIR)

	@echo "Building Linux binaries (parallel)..."

	@$(call build_linux,, $(BINARY_DIR)/placementdriver, ./placementdriver/cmd/placementdriver) &
	@$(call build_linux,-tags $(AVX512_TAGS), $(BINARY_DIR)/worker, ./worker/cmd/worker) &
	@$(call build_linux,, $(BINARY_DIR)/apigateway, ./apigateway/cmd/apigateway) &
	@$(call build_linux,, $(BINARY_DIR)/authsvc, ./auth/service/cmd/auth) &
	@$(call build_linux,, $(BINARY_DIR)/reranker, ./reranker/cmd/reranker) &

	@wait

# ==============================
# WINDOWS BUILD (PARALLEL)
# ==============================
windows: clean
	mkdir -p $(BINARY_DIR)

	@echo "Building Windows binaries (parallel)..."

	@$(call build_windows,, $(BINARY_DIR)/placementdriver_windows.exe, ./placementdriver/cmd/placementdriver) &
	@$(call build_windows,, $(BINARY_DIR)/worker_windows.exe, ./worker/cmd/worker) &
	@$(call build_windows,, $(BINARY_DIR)/apigateway_windows.exe, ./apigateway/cmd/apigateway) &
	@$(call build_windows,, $(BINARY_DIR)/authsvc_windows.exe, ./auth/service/cmd/auth) &
	@$(call build_windows,, $(BINARY_DIR)/reranker_windows.exe, ./reranker/cmd/reranker) &

	@wait

# ==============================
# INDIVIDUAL BUILDS (UNCHANGED BEHAVIOR)
# ==============================
build-placementdriver:
	mkdir -p $(BINARY_DIR)
	$(call build_linux,, $(BINARY_DIR)/placementdriver, ./placementdriver/cmd/placementdriver)

build-worker:
	mkdir -p $(BINARY_DIR)
	$(call build_linux,-tags $(AVX512_TAGS), $(BINARY_DIR)/worker, ./worker/cmd/worker)

build-apigateway:
	mkdir -p $(BINARY_DIR)
	$(call build_linux,, $(BINARY_DIR)/apigateway, ./apigateway/cmd/apigateway)

build-auth:
	mkdir -p $(BINARY_DIR)
	$(call build_linux,, $(BINARY_DIR)/authsvc, ./auth/service/cmd/auth)

build-reranker:
	mkdir -p $(BINARY_DIR)
	$(call build_linux,, $(BINARY_DIR)/reranker, ./reranker/cmd/reranker)

# ==============================
# TEST
# ==============================
test-e2e:
	go test -v ./tests/e2e -run 'Test' -timeout 30m

# ==============================
# CLEAN
# ==============================
clean:
	rm -rf $(BINARY_DIR)

# ==============================
# OPTIONAL (UNCHANGED LOGIC)
# ==============================
build-all: linux windows

build-both:
	mkdir -p $(BINARY_DIR)

	@$(call build_linux,, $(BINARY_DIR)/placementdriver, ./placementdriver/cmd/placementdriver)
	@$(call build_linux,-tags $(AVX512_TAGS), $(BINARY_DIR)/worker, ./worker/cmd/worker)
	@$(call build_linux,, $(BINARY_DIR)/apigateway, ./apigateway/cmd/apigateway)
	@$(call build_linux,, $(BINARY_DIR)/authsvc, ./auth/service/cmd/auth)
	@$(call build_linux,, $(BINARY_DIR)/reranker, ./reranker/cmd/reranker)

	@$(call build_windows,, $(BINARY_DIR)/placementdriver_windows.exe, ./placementdriver/cmd/placementdriver)
	@$(call build_windows,, $(BINARY_DIR)/worker_windows.exe, ./worker/cmd/worker)
	@$(call build_windows,, $(BINARY_DIR)/apigateway_windows.exe, ./apigateway/cmd/apigateway)
	@$(call build_windows,, $(BINARY_DIR)/authsvc_windows.exe, ./auth/service/cmd/auth)
	@$(call build_windows,, $(BINARY_DIR)/reranker_windows.exe, ./reranker/cmd/reranker)
