# OpenVpnTools 构建入口(Linux/macOS 开发环境用;Windows 用 build.ps1)
.PHONY: all web build linux test clean

all: web linux

web:
	cd web && npm install --no-fund --no-audit && npm run build

build:
	go build -o bin/ovpn-web ./cmd/ovpn-web

test:
	go vet ./...
	go test ./...

linux: linux-amd64 linux-arm64

linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/ovpn-web-linux-amd64 ./cmd/ovpn-web

linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/ovpn-web-linux-arm64 ./cmd/ovpn-web

clean:
	rm -rf bin dist web/dist
