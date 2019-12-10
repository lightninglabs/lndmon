PKG := github.com/lightninglabs/lndmon
ESCPKG := github.com\/lightninglabs\/lndmon

LINT_PKG := github.com/golangci/golangci-lint/cmd/golangci-lint

GO_BIN := ${GOPATH}/bin
LINT_BIN := $(GO_BIN)/golangci-lint

LINT_COMMIT := v1.18.0

DEPGET := cd /tmp && GO111MODULE=on go get -v
GOBUILD := GO111MODULE=on go build -v

GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
GOLIST := go list -deps $(PKG)/... | grep '$(PKG)'| grep -v '/vendor/'

RM := rm -f
CP := cp
MAKE := make

LINT = $(LINT_BIN) run -v

default: build

all: lint build

# ============
# DEPENDENCIES
# ============

$(LINT_BIN):
	@$(call print, "Fetching linter")
	$(DEPGET) $(LINT_PKG)@$(LINT_COMMIT)

# ============
# INSTALLATION
# ============

build:
	@$(call print, "Building lndmon.")
	$(GOBUILD) $(PKG)/cmd/lndmon

# =========
# UTILITIES
# =========
fmt:
	@$(call print, "Formatting source.")
	gofmt -l -w -s $(GOFILES_NOVENDOR)

lint: $(LINT_BIN)
	@$(call print, "Linting source.")
	$(LINT)

list:
	@$(call print, "Listing commands.")
	@$(MAKE) -qp | \
		awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {split($$1,A,/ /);for(i in A)print A[i]}' | \
		grep -v Makefile | \
		sort
clean:
	@$(call print, "Cleaning source.$(NC)")
	$(RM) ./lndmon
