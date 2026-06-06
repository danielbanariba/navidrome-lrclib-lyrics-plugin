# Navidrome LRCLIB Lyrics Plugin

A [Navidrome](https://www.navidrome.org/) plugin that fetches **synced (LRC)** and **plain** lyrics from [LRCLIB](https://lrclib.net), the free, open, no-API-key lyrics database.

It implements Navidrome's `Lyrics` plugin capability. The plugin returns LRCLIB's raw lyrics string verbatim — Navidrome parses LRC timestamps host-side, so synced lyrics "just work".

> Verified end-to-end against Navidrome 0.61.2: the plugin loads with `capabilities=[Lyrics]` and returns synced lyrics from LRCLIB.

## How it works

For each lyrics request Navidrome sends the track's metadata, and the plugin:

1. Tries an exact match via `GET /api/get` using `artist_name`, `track_name`, `album_name` and `duration`.
2. Falls back to `GET /api/search` (by track + artist) if the exact match misses.
3. Prefers **synced** lyrics; uses **plain** lyrics otherwise; returns nothing for instrumental tracks (so Navidrome moves on to the next source).

LRCLIB requires **no API key** and has **no rate limit**. The plugin only needs outbound HTTP to `lrclib.net`.

## Build

Requires **TinyGo 0.41.1 or newer**. TinyGo must support the Go version the Navidrome PDK requires (currently Go 1.25+).

> ⚠️ TinyGo **0.37 does not work**: it only supports Go ≤ 1.24, while the plugin PDK's `go.mod` requires `go >= 1.25`. If your distro ships an old TinyGo, grab a recent release from <https://github.com/tinygo-org/tinygo/releases>.

```sh
make            # produces lrclib.ndp
# or, if TinyGo isn't on PATH:
make TINYGO=~/.local/tinygo-0.41.1/bin/tinygo
```

## Install

1. **Build** `lrclib.ndp` (above) and copy it into your Navidrome plugins folder (`Plugins.Folder`, default `<DataFolder>/plugins`).

2. **Enable the plugin system** in `navidrome.toml`:

   ```toml
   [Plugins]
   Enabled = true
   ```

   On versions where the plugin system is still experimental, also set the top-level flag:

   ```toml
   DevEnablePlugins = true
   ```

3. **Enable this plugin.** Plugins are registered **disabled** by default — this step is required, or the plugin is never loaded. Enable `lrclib` from the Navidrome admin UI (Settings → Plugins), or via the Native API. You can confirm it loaded when the log shows:

   ```
   Loaded plugin  capabilities="[Lyrics]"  plugin=lrclib
   ```

4. **Add the plugin to your lyrics priority.** The default `LyricsPriority` (`embedded,.lrc,.txt`) contains no plugin, so the plugin would never be consulted:

   ```toml
   LyricsPriority = "embedded,.lrc,.txt,lrclib"
   ```

   (`lrclib` is the plugin id, derived from the `.ndp` filename.)

5. **Restart** Navidrome (or rely on `Plugins.AutoReload`). Note: changing the `.ndp` file re-registers the plugin as **disabled**, so re-enable it after replacing the file.

## Notes

- LRCLIB does not expose a language, so the `lang` field is reported as `xxx`.
- The first lyrics lookup after a plugin (re)load can be slow while the WASM sandbox warms up its HTTP/TLS path; subsequent lookups are faster.

## Credits

- Lyrics data from [LRCLIB](https://lrclib.net) — please consider [contributing lyrics](https://lrclib.net) back to them.
- Built on the Navidrome plugin PDK.

## License

MIT — see [LICENSE](./LICENSE).
