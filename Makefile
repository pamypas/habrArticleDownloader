# Makefile for habrdownloader

# Binary name
BINARY := habrdownloader

# Source files (add more if needed)
SRC := main.go

# Default target
.PHONY: all
all: build

# Build the binary
.PHONY: build
build:
	go build -o $(BINARY) $(SRC)

# Run the program (use ARGS="..." to pass command‑line arguments)
.PHONY: run
run: build
	./$(BINARY) $(ARGS)

# Clean generated files
.PHONY: clean
clean:
	rm -f $(BINARY)

# Update module dependencies
.PHONY: tidy
tidy:
	go mod tidy
