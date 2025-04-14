# Makefile for pgkit

.PHONY: test

# Run Go tests
test:
	@echo "Running Go tests..."
	@go test ./...