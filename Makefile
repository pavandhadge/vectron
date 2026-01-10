.PHONY: build clean

build: clean
	mkdir -p ./bin
	go build -o ./bin/placementdriver ./placementdriver/cmd/placementdriver
	go build -o ./bin/worker ./worker/cmd/worker
	go build -o ./bin/apigateway ./apigateway/cmd/apigateway
	go build -o ./bin/authsvc ./auth/service/cmd/auth

build-placementdriver:
	mkdir -p ./bin
	go build -o ./bin/placementdriver ./placementdriver/cmd/placementdriver

build-worker:
	mkdir -p ./bin
	go build -o ./bin/worker ./worker/cmd/worker

build-apigateway:
	mkdir -p ./bin
	go build -o ./bin/apigateway ./apigateway/cmd/apigateway

build-auth:
	mkdir -p ./bin
	go build -o ./bin/authsvc ./auth/service/cmd/auth

clean:
	rm -rf ./bin
