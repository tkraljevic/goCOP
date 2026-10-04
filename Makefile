GOLANGCI_VERSION := v2.14.0
GREMLINS_VERSION := v0.6.0
QUALITY_ROOT := $(CURDIR)
QUALITY_OUT ?= $(QUALITY_ROOT)/.quality
QUALITY_BASELINE ?= $(QUALITY_ROOT)/quality/baseline.json

.PHONY: quality quality-tools quality-report quality-test quality-baseline quality-deep

quality-tools:
	GOBIN="$(QUALITY_ROOT)/bin/quality" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

quality-test:
	cd dev/quality && go test -race ./...

quality: quality-test
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -out "$(QUALITY_OUT)" -baseline "$(QUALITY_BASELINE)"

quality-report: quality-test
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -out "$(QUALITY_OUT)" -baseline "$(QUALITY_BASELINE)" -report-only

# Izričita odluka nakon pregleda diff-a izvještaja, nikad automatski u PR-u.
quality-baseline:
	@test "$(ACCEPT_BASELINE)" = "yes" || (echo 'Potrebno: make quality-baseline ACCEPT_BASELINE=yes'; exit 1)
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -out "$(QUALITY_OUT)" -baseline "$(QUALITY_BASELINE)" -record-baseline

# Isključivo zasebna čista kopija: Gremlins mijenja izvorne datoteke.
quality-deep:
	@test -n "$(MUTATION_ROOT)" || (echo 'Postavite MUTATION_ROOT na zasebnu čistu kopiju; vidi docs/CODE_QUALITY.md'; exit 1)
	GOBIN="$(QUALITY_ROOT)/bin/quality" go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -mutation-root "$(MUTATION_ROOT)" -mutation-package "$(MUTATION_PACKAGE)" -out "$(QUALITY_OUT)"
