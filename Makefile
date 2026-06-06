PLUGIN := lrclib

# Build requires TinyGo. Use TinyGo 0.41.1+ — it must support the Go version the
# Navidrome plugin PDK requires (currently Go 1.25+). TinyGo 0.37 does NOT work:
# it caps at Go 1.24, while the PDK's go.mod requires `go >= 1.25`.
#
# Override the binary if TinyGo is not on your PATH, e.g.:
#   make TINYGO=~/.local/tinygo-0.41.1/bin/tinygo
TINYGO ?= tinygo

.PHONY: all clean

all: $(PLUGIN).ndp

plugin.wasm: main.go manifest.json go.mod
	$(TINYGO) build -o plugin.wasm -target wasip1 -buildmode=c-shared .

$(PLUGIN).ndp: plugin.wasm manifest.json
	zip -j $(PLUGIN).ndp manifest.json plugin.wasm

clean:
	rm -f plugin.wasm $(PLUGIN).ndp
