# 从 go.mod 提取 module 名的最后一部分作为二进制文件名
BINARY_NAME := $(shell grep '^module' go.mod | awk '{print $$2}' | xargs basename)
BIN_DIR := bin

.PHONY: build clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd

clean:
	rm -rf $(BIN_DIR)
