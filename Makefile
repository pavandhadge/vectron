.PHONY: build clean

build: clean
	mkdir -p ./bin
	go build -o ./bin/placementdriver ./placementdriver/cmd/placementdriver
	go build -o ./bin/worker ./worker/cmd/worker
	go build -o ./bin/apigateway ./apigateway/cmd/apigateway

build-placementdriver:
	mkdir -p ./bin
	go build -o ./bin/placementdriver ./placementdriver/cmd/placementdriver

build-worker:
	mkdir -p ./bin
	go build -o ./bin/worker ./worker/cmd/worker

build-apigateway:
	mkdir -p ./bin
	go build -o ./bin/apigateway ./apigateway/cmd/apigateway

clean:
	rm -rf ./bin
