# Common settings
BINARY_DIR := ./bin
GO_BUILD_FLAGS := -trimpath   # optional: cleaner builds

.PHONY: all build clean windows linux \
        build-placementdriver build-worker build-apigateway build-auth build-reranker

all: build windows

build: clean linux

linux: clean
	mkdir -p $(BINARY_DIR)
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/placementdriver ./placementdriver/cmd/placementdriver
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/worker        ./worker/cmd/worker
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/apigateway   ./apigateway/cmd/apigateway
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/authsvc      ./auth/service/cmd/auth
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/reranker     ./reranker/cmd/reranker

windows: clean
	mkdir -p $(BINARY_DIR)
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/placementdriver_windows.exe ./placementdriver/cmd/placementdriver
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/worker_windows.exe        ./worker/cmd/worker
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/apigateway_windows.exe   ./apigateway/cmd/apigateway
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/authsvc_windows.exe      ./auth/service/cmd/auth
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/reranker_windows.exe     ./reranker/cmd/reranker

# Per-component targets (Linux by default, no suffix)
build-placementdriver:
	mkdir -p $(BINARY_DIR)
	go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/placementdriver ./placementdriver/cmd/placementdriver

build-worker:
	mkdir -p $(BINARY_DIR)
	go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/worker ./worker/cmd/worker

build-apigateway:
	mkdir -p $(BINARY_DIR)
	go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/apigateway ./apigateway/cmd/apigateway

build-auth:
	mkdir -p $(BINARY_DIR)
	go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/authsvc ./auth/service/cmd/auth

build-reranker:
	mkdir -p $(BINARY_DIR)
	go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/reranker ./reranker/cmd/reranker

clean:
	rm -rf $(BINARY_DIR)

# Optional shortcuts
build-all: linux windows

# Optional: build both platforms without cleaning in between
build-both:
	mkdir -p $(BINARY_DIR)
	GOOS=linux   GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/placementdriver          ./placementdriver/cmd/placementdriver
	GOOS=linux   GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/worker                   ./worker/cmd/worker
	GOOS=linux   GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/apigateway              ./apigateway/cmd/apigateway
	GOOS=linux   GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/authsvc                 ./auth/service/cmd/auth
	GOOS=linux   GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/reranker                ./reranker/cmd/reranker
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/placementdriver_windows.exe ./placementdriver/cmd/placementdriver
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/worker_windows.exe           ./worker/cmd/worker
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/apigateway_windows.exe      ./apigateway/cmd/apigateway
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/authsvc_windows.exe         ./auth/service/cmd/auth
	GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY_DIR)/reranker_windows.exe        ./reranker/cmd/reranker
