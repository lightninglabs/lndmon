PKG := github.com/lightninglabs/lndmon
ESCPKG := github.com\/lightninglabs\/lndmon
TOOLS_DIR := tools

LINT_PKG := github.com/golangci/golangci-lint/cmd/golangci-lint
GOIMPORTS_PKG := github.com/rinchsan/gosimports/cmd/gosimports

GO_BIN := ${GOPATH}/bin
LINT_BIN := $(GO_BIN)/golangci-lint

GOBUILD := go build -v
GOTEST := go test -v

GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
GOLIST := go list -deps $(PKG)/... | grep '$(PKG)'| grep -v '/vendor/'

RM := rm -f
CP := cp
MAKE := make
DOCKER_TOOLS = docker run -v $$(pwd):/build lndmon-tools

DOCKER_IMAGE := lndmon
# Derive the tag from the current commit, appending "-dirty" if the working
# tree has uncommitted changes. --match='__no_such_tag__' forces git to ignore
# all tags and fall back to the short commit hash via --always.
DOCKER_COMMIT := $(shell git describe --always --dirty --match='__no_such_tag__')
DOCKER_TAG := $(DOCKER_COMMIT)

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

# =======
# TESTING
# =======

test:
	@$(call print, "Running unit tests.")
	$(GOTEST) ./...

# =========
# UTILITIES
# =========
docker-build:
	@$(call print, "Building lndmon docker image.")
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-push:
	@$(call print, "Pushing lndmon docker image.")
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

docker-build-push: docker-build docker-push

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
