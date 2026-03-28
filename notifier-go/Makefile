.PHONY: all build clean run

# Default OS and ARCH
GOOS ?= windows
GOARCH ?= amd64
BINARY_NAME = notifier.exe

all: build

build:
	@echo "Building Go Notifier for Windows..."
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-H windowsgui" -o $(BINARY_NAME)

clean:
	@echo "Cleaning notifier build..."
	rm -f $(BINARY_NAME)

run:
	@echo "Notifier runs on Windows only. Please double-click notifier.exe on your Windows host to run."
