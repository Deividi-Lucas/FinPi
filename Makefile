.PHONY: all build build-arm64 run clean

APP_NAME=finpi

all: build

build:
	go build -ldflags="-s -w" -o $(APP_NAME) .

# Cross-compilação para Raspberry Pi 3 Model B (Ubuntu Server ARM64)
build-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(APP_NAME)-arm64 .

run:
	go run .

clean:
	rm -f $(APP_NAME) $(APP_NAME)-arm64 finpi.db*
