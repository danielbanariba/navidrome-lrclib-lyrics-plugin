# Navidrome LRCLIB Lyrics Plugin

A [Navidrome](https://www.navidrome.org/) plugin that fetches **synced (LRC)** and **plain** lyrics from [LRCLIB](https://lrclib.net), the free, open, no-API-key lyrics database.

It implements Navidrome's `Lyrics` plugin capability. The plugin returns LRCLIB's raw lyrics string verbatim — Navidrome parses LRC timestamps host-side, so synced lyrics "just work".

## How it works

For each lyrics request Navidrome sends the track's metadata, and the plugin:

1. Tries an exact match via `GET /api/get` using `artist_name`, `track_name`, `album_name` and `duration`.
2. Falls back to `GET /api/search` (by track + artist) if the exact match misses.
3. Prefers **synced** lyrics; uses **plain** lyrics otherwise; returns nothing for instrumental tracks (so Navidrome moves on to the next source).

LRCLIB requires **no API key** and has **no rate limit**. The plugin only needs outbound HTTP to `lrclib.net`.

## Build

Requires Go 1.24+ (for `//go:wasmexport` + `-buildmode=c-shared` on `wasip1`).

```sh
make            # produces lrclib.ndp
```

> TinyGo also works if your TinyGo version supports your Go toolchain. As of this writing TinyGo 0.37 does not support Go 1.26, so the `Makefile` uses the standard Go `wasip1` toolchain. Both produce a compatible WASM module.

## Install

1. Copy `lrclib.ndp` into your Navidrome plugins folder (`Plugins.Folder`, default `<DataFolder>/plugins`).
2. Make sure plugins are enabled in `navidrome.toml`:

   ```toml
   [Plugins]
   Enabled = true
   ```

3. **Add the plugin to your lyrics priority** — this is required, otherwise the plugin is never consulted. The default `LyricsPriority` is `embedded,.lrc,.txt` and contains no plugin:

   ```toml
   LyricsPriority = "embedded,.lrc,.txt,lrclib"
   ```

   (`lrclib` is the plugin id, derived from the `.ndp` filename.)

4. Restart Navidrome (or rely on `Plugins.AutoReload`).

## Credits

- Lyrics data from [LRCLIB](https://lrclib.net) — please consider [contributing lyrics](https://lrclib.net) back to them.
- Built on the Navidrome plugin PDK.

## License

MIT — see [LICENSE](./LICENSE).
