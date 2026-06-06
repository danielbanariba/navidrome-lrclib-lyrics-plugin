# Navidrome LRCLIB Lyrics Plugin

A [Navidrome](https://www.navidrome.org/) plugin that fetches **synced (LRC)** and **plain** lyrics from [LRCLIB](https://lrclib.net), the free, open, no-API-key lyrics database.

It implements Navidrome's `Lyrics` plugin capability. The plugin returns LRCLIB's raw lyrics string verbatim — Navidrome parses LRC timestamps host-side, so synced lyrics "just work".

> Verified end-to-end against Navidrome 0.61.2: the plugin loads with `capabilities=[Lyrics]` and returns synced lyrics from LRCLIB.

## How it works

For each lyrics request Navidrome sends the track's metadata, and the plugin:

1. **Checks its KVStore cache** first — a hit (lyrics, or a recorded "no lyrics") is returned immediately, without touching the network.
2. On a miss, tries an exact match via `GET /api/get` using `artist_name`, `track_name`, `album_name` and `duration`.
3. Falls back to `GET /api/search` (by track + artist) if the exact match misses.
4. Prefers **synced** lyrics; uses **plain** lyrics otherwise; returns nothing for instrumental tracks (so Navidrome moves on to the next source).
5. Caches the result (lyrics for 30 days, misses for 7 days) so the next lookup for that track is instant.

LRCLIB requires **no API key** and has **no rate limit**. The plugin needs outbound HTTP to `lrclib.net` and the `kvstore` permission for its cache.

### Caching

LRCLIB's API is functional but **slow server-side** (~7–10 s per request — measured against the live API, independent of Navidrome). To keep that cost off the hot path, results are stored in the plugin's isolated KVStore:

- First lookup for a track: pays the LRCLIB latency once (~7–10 s).
- Every subsequent lookup for that track: served from cache in **single-digit milliseconds** (~800× faster, verified end-to-end).

The cache is capped at 25 MB (`permissions.kvstore.maxSize`); cache write failures are non-fatal — lyrics are still returned, they just aren't persisted.

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
- The first lookup for any track is slow because LRCLIB itself is slow (~7–10 s, server-side — not the plugin or the WASM sandbox). The cache makes every later lookup for that track instant.

## Credits

- Lyrics data from [LRCLIB](https://lrclib.net) — please consider [contributing lyrics](https://lrclib.net) back to them.
- Built on the Navidrome plugin PDK.

## License

MIT — see [LICENSE](./LICENSE).
