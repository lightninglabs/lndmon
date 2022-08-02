PKG := github.com/lightninglabs/lndmon
ESCPKG := github.com\/lightninglabs\/lndmon
TOOLS_DIR := tools

LINT_PKG := github.com/golangci/golangci-lint/cmd/golangci-lint
GOIMPORTS_PKG := github.com/rinchsan/gosimports/cmd/gosimports

GO_BIN := ${GOPATH}/bin
LINT_BIN := $(GO_BIN)/golangci-lint

GOBUILD := go build -v

GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
GOLIST := go list -deps $(PKG)/... | grep '$(PKG)'| grep -v '/vendor/'

RM := rm -f
CP := cp
MAKE := make
DOCKER_TOOLS = docker run -v $$(pwd):/build lndmon-tools

LINT = $(LINT_BIN) run -v

default: build

all: lint build

# ============
# DEPENDENCIES
# ============

goimports:
	@$(call print, "Installing goimports.")
	cd $(TOOLS_DIR); go install -trimpath -tags=tools $(GOIMPORTS_PKG)

# ============
# INSTALLATION
# ============

build:
	@$(call print, "Building lndmon.")
	$(GOBUILD) $(PKG)/cmd/lndmon

# =========
# UTILITIES
# =========
docker-tools:
	@$(call print, "Building tools docker image.")
	docker build -q -t lndmon-tools $(TOOLS_DIR)

fmt: goimports
	@$(call print, "Fixing imports.")
	gosimports -w $(GOFILES_NOVENDOR)
	@$(call print, "Formatting source.")
	gofmt -l -w -s $(GOFILES_NOVENDOR)

lint: docker-tools
	@$(call print, "Linting source.")
	$(DOCKER_TOOLS) golangci-lint run -v $(LINT_WORKERS)

list:
	@$(call print, "Listing commands.")
	@$(MAKE) -qp | \
		awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {split($$1,A,/ /);for(i in A)print A[i]}' | \
		grep -v Makefile | \
		sort
clean:
	@$(call print, "Cleaning source.$(NC)")
	$(RM) ./lndmon
