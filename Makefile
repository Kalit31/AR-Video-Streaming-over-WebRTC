# Variables
APP_NAME = main
BIN_DIR = bin
GO_FILES = *.go

# Create the bin directory if it doesn't exist
.PHONY: build run-server run-client clean all
build:
	mkdir -p $(BIN_DIR)  # Create bin directory if it doesn't exist
	go build -o $(BIN_DIR)/$(APP_NAME) $(GO_FILES)

# Run the server
run-server: 
	./$(BIN_DIR)/$(APP_NAME) --server

# Run the client
run-client: 
	./$(BIN_DIR)/$(APP_NAME) --client

# Clean up build artifacts
clean:
	rm -f $(BIN_DIR)/$(APP_NAME)

# Default target
all: build