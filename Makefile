GO ?= go
PKGS ?= ./...
BENCH ?= .
BENCHTIME ?=
BENCHFLAGS ?=

.PHONY: check test test-native test-emulated vet race bench

check: test vet

test: test-native test-emulated

test-native:
	GOEXPERIMENT=simd $(GO) test $(PKGS)

test-emulated:
	GOEXPERIMENT=simd GODEBUG=simd=0 $(GO) test $(PKGS)

vet:
	GOEXPERIMENT=simd $(GO) vet $(PKGS)

race:
	GOEXPERIMENT=simd $(GO) test -race $(PKGS)

bench:
	GOEXPERIMENT=simd $(GO) test -run='^$$' -bench='$(BENCH)' -benchmem $(if $(BENCHTIME),-benchtime=$(BENCHTIME)) $(BENCHFLAGS) $(PKGS)
