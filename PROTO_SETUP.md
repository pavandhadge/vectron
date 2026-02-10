# Proto Setup (Linux, macOS, Windows)

This guide is the single source of truth for setting up Protocol Buffers + plugins for this repo and generating all code.

**Proto sources in this repo**
- `shared/proto/*/*.proto` (authoritative server-side APIs)
- `clientlibs/js/proto/apigateway/apigateway.proto` (JS client copy for packaging)

**Generated outputs**
- Go server + gateway: `shared/proto/**`
- Go client: `clientlibs/go/proto/**`
- OpenAPI: `apigateway/proto/openapi/**`
- JS client: `clientlibs/js/proto/**`
- Python client: `clientlibs/python/vectron_client/proto/**`

## 1) Install Protoc

Pick the section for your OS.

**Linux (Debian/Ubuntu)**
```bash
sudo apt-get update
sudo apt-get install -y protobuf-compiler
protoc --version
```

**macOS (Homebrew)**
```bash
brew install protobuf
protoc --version
```

**Windows**
Use one of the following and then verify `protoc` in a new terminal.
```powershell
winget install --id Google.Protobuf -e
protoc --version
```
Or with Chocolatey:
```powershell
choco install protoc
protoc --version
```
Or with Scoop:
```powershell
scoop install protobuf
protoc --version
```

## 2) Install Go (required for Go + gRPC + gateway plugins)

Ensure Go is installed and on PATH:
```bash
go version
go env GOPATH
```

Make sure `$(go env GOPATH)/bin` is on PATH.

**Linux/macOS (bash/zsh)**
```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc
source ~/.bashrc
```
If you use zsh, replace `~/.bashrc` with `~/.zshrc`.

**Windows (PowerShell)**
```powershell
$gopath = (go env GOPATH)
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";" + $gopath + "\bin", "User")
```
Close and reopen the terminal after setting PATH.

## 3) Install Protoc Plugins (Go + gRPC + gateway + OpenAPI)

These are required by `generate-all.sh`.
```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.27.7
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.27.7
```

Verify:
```bash
protoc-gen-go --version
protoc-gen-go-grpc --version
protoc-gen-grpc-gateway --version
protoc-gen-openapiv2 --version
```

## 4) Install JS and Python Tooling (client libraries)

**Node.js (for TS/JS client)**
```bash
node --version
npm --version
```
Install deps in the JS client:
```bash
cd clientlibs/js
npm install
cd ../..
```
This provides `protoc-gen-ts_proto` at `clientlibs/js/node_modules/.bin/protoc-gen-ts_proto`.

**Python (for Python client)**
```bash
python3 --version
python3 -m pip install --upgrade pip
python3 -m pip install grpcio-tools
```

## 5) Ensure Go Module Cache Has Google APIs + grpc-gateway protos

The generation script includes these proto import paths:
- `github.com/googleapis/googleapis`
- `github.com/grpc-ecosystem/grpc-gateway/v2`

Populate the module cache:
```bash
go mod download
```

## 6) Generate All Protos

Use the repo script:
```bash
./generate-all.sh
```

**Windows note:** `generate-all.sh` is a bash script. Use Git Bash or WSL.

## 7) Troubleshooting

**`protoc-gen-...: program not found or is not executable`**
- Ensure `$(go env GOPATH)/bin` is on PATH.
- Reopen your terminal after PATH updates.

**`google/api/annotations.proto: File not found`**
- Run `go mod download`.
- Verify the include path exists:
```bash
ls "$(go env GOPATH)/pkg/mod/github.com/googleapis/googleapis@"*
```

**`protoc-gen-ts_proto not found`**
- Run `npm install` in `clientlibs/js`.

**Windows: `./generate-all.sh: command not found`**
- Use Git Bash or WSL, or run the commands manually from `generate-all.sh`.

## 8) Optional: Regenerate Just One Target

If you need partial regeneration, open `generate-all.sh` and run only the section you need.

