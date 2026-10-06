BIN := $(HOME)/.local/bin/cmon

build:
	go build -ldflags "-s -w" -o bin/cmon .

test:
	go test ./...

# copies the binary and registers hooks + login agent (rerun after every change)
install: build
	install -m 755 bin/cmon $(BIN)
	$(BIN) install

uninstall:
	$(BIN) uninstall
	rm -f $(BIN)

.PHONY: build test install uninstall
