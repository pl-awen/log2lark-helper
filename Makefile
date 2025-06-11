GOHOSTOS:=$(shell go env GOHOSTOS)
GOPATH:=$(shell go env GOPATH)
VERSION=$(shell git describe --tags --always)

.PHONY: build
# build
build:
	go build -ldflags "-X main.Version=$(VERSION)" -o ./build/ ./cmd/log2lark-helper.go
