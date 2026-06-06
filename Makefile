PLUGIN := lrclib

# NOTE: TinyGo 0.37 does not yet support Go 1.26, so this Makefile uses the
# standard Go wasip1 toolchain (Go 1.24+ supports //go:wasmexport and
# -buildmode=c-shared). If you have a TinyGo build that matches your Go version,
# `tinygo build -o plugin.wasm -target wasip1 -buildmode=c-shared .` also works.

.PHONY: all clean

all: $(PLUGIN).ndp

plugin.wasm: main.go manifest.json go.mod
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .

$(PLUGIN).ndp: plugin.wasm manifest.json
	zip -j $(PLUGIN).ndp manifest.json plugin.wasm

clean:
	rm -f plugin.wasm $(PLUGIN).ndp
