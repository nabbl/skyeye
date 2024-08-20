GO = go

# Detect CPU architecture
ifeq ($(shell uname -m),arm64) 
GOARCH = arm64
else ifeq ($(shell uname -m),x86_64) 
GOARCH = amd64
endif

# Detect OS
ifeq ($(OS),Windows_NT)
OS_DISTRIBUTION := Windows
else ifeq ($(shell uname -s),Darwin)
OS_DISTRIBUTION := macOS
else
OS_DISTRIBUTION := $(shell lsb_release -si)
endif

# Override Windows Go environment with MSYS2 UCRT64 Go environment
MSYS2_GOPATH = /ucrt64
MSYS2_GOROOT = /ucrt64/lib/go
MSYS2_GO = /ucrt64/bin/go
ifeq ($(OS_DISTRIBUTION),Windows)
GO = $(MSYS2_GO)
endif

# Source code paths
SKYEYE_PATH = $(shell pwd)
SKYEYE_SOURCES = $(shell find . -type f -name '*.go')
SKYEYE_SOURCES += go.mod go.sum
SKYEYE_EXE = skyeye.exe
SKYEYE_ELF = skyeye

WHISPER_CPP_PATH = third_party/whisper.cpp
LIBWHISPER_PATH = $(WHISPER_CPP_PATH)/libwhisper.a
WHISPER_H_PATH = $(WHISPER_CPP_PATH)/whisper.h
WHISPER_CPP_VERSION = v1.6.2

# Compiler variables and flags
BUILD_VARS = CGO_ENABLED=1 \
  C_INCLUDE_PATH="$(SKYEYE_PATH)/$(WHISPER_CPP_PATH)" \
  LIBRARY_PATH="$(SKYEYE_PATH)/$(WHISPER_CPP_PATH)" \
  GOARCH=$(GOARCH)
BUILD_FLAGS = -tags nolibopusfile

# Populate --version from Git tag
LDFLAGS= -X "main.Version=$(shell git describe --tags || echo devel)"

ifeq ($(OS_DISTRIBUTION),Windows)
# Static linking on Windows to avoid MSYS2 dependency at runtime
CFLAGS = $(pkg-config opus soxr --cflags --static)
BUILD_VARS += CFLAGS=$(CFLAGS)
EXTLDFLAGS = $(pkg-config opus soxr --libs --static)
LDFLAGS += -linkmode external -extldflags "$(EXTLDFLAGS) -static -fopenmp"
endif

BUILD_VARS += LDFLAGS='$(LDFLAGS)'
BUILD_FLAGS += -ldflags '$(LDFLAGS)'

.PHONY: default
ifeq ($(OS_DISTRIBUTION),Windows)
default: $(SKYEYE_EXE)
else
default: $(SKYEYE_ELF)
endif

.PHONY: install-msys2-dependencies
install-msys2-dependencies:
	pacman -Syu --needed \
	  git \
	  base-devel \
	  $(MINGW_PACKAGE_PREFIX)-toolchain \
	  $(MINGW_PACKAGE_PREFIX)-go \
	  $(MINGW_PACKAGE_PREFIX)-opus \
	  $(MINGW_PACKAGE_PREFIX)-libsoxr

.PHONY: install-arch-linux-dependencies
install-arch-linux-dependencies:
	sudo pacman -Syu \
	  git \
	  base-devel \
	  go \
	  opus \
	  soxr

.PHONY: install-debian-dependencies
install-debian-dependencies:
	sudo apt-get update
	sudo apt-get install -y \
	  git \
	  golang-go \
	  libopus-dev \
	  libopus0 \
	  libsoxr-dev \
	  libsoxr0

.PHONY: install-macos-dependencies
install-macos-dependencies:
	brew install \
	  git \
	  opus \
	  libsoxr

$(LIBWHISPER_PATH) $(WHISPER_H_PATH):
	if [[ ! -f $(LIBWHISPER_PATH) || ! -f $(WHISPER_H_PATH) ]]; then git -C "$(WHISPER_CPP_PATH)" checkout --quiet $(WHISPER_CPP_VERSION) || git clone --depth 1 --branch $(WHISPER_CPP_VERSION) -c advice.detachedHead=false https://github.com/ggerganov/whisper.cpp.git "$(WHISPER_CPP_PATH)" && make -C $(WHISPER_CPP_PATH)/bindings/go whisper; fi

.PHONY: whisper
whisper: $(LIBWHISPER_PATH) $(WHISPER_H_PATH)

.PHONY: generate
generate:
	$(BUILD_VARS) $(GO) generate $(BUILD_FLAGS) ./...

$(SKYEYE_EXE): generate $(SKYEYE_SOURCES) $(LIBWHISPER_PATH) $(WHISPER_H_PATH)
	GOROOT="$(MSYS2_GOROOT)" GOPATH="$(MSYS2_GOPATH)" $(BUILD_VARS) $(GO) build $(BUILD_FLAGS) ./cmd/skyeye/

$(SKYEYE_ELF): generate $(SKYEYE_SOURCES) $(LIBWHISPER_PATH) $(WHISPER_H_PATH)
	$(BUILD_VARS) $(GO) build $(BUILD_FLAGS) ./cmd/skyeye/


.PHONY: test
test: generate
	$(BUILD_VARS) $(GO) run gotest.tools/gotestsum -- $(BUILD_FLAGS) ./...

.PHONY: vet
vet: generate
	$(BUILD_VARS) $(GO) vet $(BUILD_FLAGS) ./...

# Note: Running golangci-lint from source like this is not recommended, see https://golangci-lint.run/welcome/install/#install-from-source
# Don't use this make target in CI, it's not guaranteed to be accurate. Provided for convenience only.
.PHONY: lint
lint:
	$(BUILD_VARS) $(GO) run $(BUILD_FLAGS) github.com/golangci/golangci-lint/cmd/golangci-lint run ./...

.PHONY: mostlyclean
mostlyclean:
	rm -f "$(SKYEYE_EXE)" "$(SKYEYE_ELF)"
	find . -type f -name 'mock_*.go' -delete

.PHONY: clean
clean: mostlyclean
	rm -rf "$(WHISPER_CPP_PATH)"
