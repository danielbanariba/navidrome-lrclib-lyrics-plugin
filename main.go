// Navidrome LRCLIB lyrics plugin.
//
// Implements the Navidrome Lyrics capability by fetching synced (LRC) or plain
// lyrics from the LRCLIB public API (https://lrclib.net). The plugin returns the
// raw lyrics string verbatim; Navidrome parses LRC timestamps host-side
// (model.ToLyrics), so the plugin must NOT build synced-line structures itself.
//
// LRCLIB's API is functional but slow (~7-10s per request, server-side), so
// results are cached in the per-plugin KVStore: the first lookup for a track
// pays the LRCLIB latency, every subsequent lookup is served from cache.
//
// Build (see Makefile): make            # produces lrclib.ndp
package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/lyrics"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const (
	apiBase   = "https://lrclib.net/api"
	userAgent = "navidrome-lrclib-lyrics-plugin/0.2.0 (https://github.com/danielbanariba/navidrome-lrclib-lyrics-plugin)"
	// LRCLIB is slow (~7-10s server-side); give a single request enough room to
	// complete instead of timing out and paying the fallback search on top.
	httpTimeoutMs = 15000

	cachePrefix = "lrclib:v1:"
	// Lyrics rarely change, so cache hits live a long time. Misses expire sooner
	// so newly-added LRCLIB lyrics get picked up without a manual cache clear.
	ttlHitSeconds  int64 = 30 * 24 * 60 * 60 // 30 days
	ttlMissSeconds int64 = 7 * 24 * 60 * 60  // 7 days

	cacheValHit  byte = 'H' // value is 'H' + raw lyrics text
	cacheValMiss byte = 'M' // value is 'M' (no lyrics for this track)
)

// lrclibTrack is the subset of the LRCLIB response we consume.
// Both /api/get and /api/search return objects of this shape.
type lrclibTrack struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

type lrclibPlugin struct{}

// Compile-time assertion that we satisfy the Lyrics capability.
var _ lyrics.Lyrics = (*lrclibPlugin)(nil)

func init() {
	lyrics.Register(&lrclibPlugin{})
}

// GetLyrics looks up lyrics for the given track, serving from the KVStore cache
// when possible and falling back to LRCLIB otherwise.
func (p *lrclibPlugin) GetLyrics(req lyrics.GetLyricsRequest) (lyrics.GetLyricsResponse, error) {
	t := req.Track
	if t.Title == "" {
		return lyrics.GetLyricsResponse{}, nil
	}

	artist := t.Artist
	if artist == "" && len(t.Artists) > 0 {
		artist = t.Artists[0].Name
	}

	key := cacheKey(artist, t.Title, t.Album, t.Duration)

	// 1) Cache: a hit (lyrics or a recorded miss) avoids the slow LRCLIB call.
	if text, found, miss := cacheGet(key); found {
		if miss {
			return lyrics.GetLyricsResponse{}, nil
		}
		return response(text), nil
	}

	// 2) Miss: query LRCLIB, then record the result (text or miss) for next time.
	text := ""
	if track := p.lookup(t.Title, artist, t.Album, t.Duration); track != nil {
		text = pickLyrics(track)
	}
	cacheStore(key, text)

	if text == "" {
		return lyrics.GetLyricsResponse{}, nil
	}
	return response(text), nil
}

func response(text string) lyrics.GetLyricsResponse {
	// Raw text (plain or LRC). LRCLIB has no language, so Lang is left empty;
	// the host defaults it to "xxx".
	return lyrics.GetLyricsResponse{Lyrics: []lyrics.LyricsText{{Text: text}}}
}

// lookup tries the exact endpoint first, then the search fallback.
func (p *lrclibPlugin) lookup(title, artist, album string, duration float32) *lrclibTrack {
	if track, err := p.apiGet(title, artist, album, duration); err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("lrclib: /api/get error: %v", err))
	} else if track != nil {
		return track
	}
	if track, err := p.apiSearch(title, artist); err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("lrclib: /api/search error: %v", err))
	} else if track != nil {
		return track
	}
	return nil
}

// apiGet queries the exact-match endpoint. A 404 means "no match", not an error.
func (p *lrclibPlugin) apiGet(title, artist, album string, duration float32) (*lrclibTrack, error) {
	q := url.Values{}
	q.Set("track_name", title)
	q.Set("artist_name", artist)
	q.Set("album_name", album)
	if duration > 0 {
		q.Set("duration", strconv.Itoa(int(duration+0.5)))
	}

	body, status, err := p.get(apiBase + "/get?" + q.Encode())
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}
	if status != 200 {
		return nil, fmt.Errorf("unexpected status %d", status)
	}

	var track lrclibTrack
	if err := json.Unmarshal(body, &track); err != nil {
		return nil, fmt.Errorf("decode /api/get: %w", err)
	}
	return &track, nil
}

// apiSearch queries the fuzzy-search endpoint and returns the first result that
// actually carries lyrics.
func (p *lrclibPlugin) apiSearch(title, artist string) (*lrclibTrack, error) {
	q := url.Values{}
	q.Set("track_name", title)
	if artist != "" {
		q.Set("artist_name", artist)
	}

	body, status, err := p.get(apiBase + "/search?" + q.Encode())
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("unexpected status %d", status)
	}

	var results []lrclibTrack
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode /api/search: %w", err)
	}
	for i := range results {
		if results[i].SyncedLyrics != "" || results[i].PlainLyrics != "" {
			return &results[i], nil
		}
	}
	return nil, nil
}

// get performs a GET against the LRCLIB host service with the recommended
// User-Agent and returns the body + status.
func (p *lrclibPlugin) get(rawURL string) ([]byte, int32, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method: "GET",
		URL:    rawURL,
		Headers: map[string]string{
			"Accept":     "application/json",
			"User-Agent": userAgent,
		},
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, 0, err
	}
	if resp == nil {
		return nil, 0, fmt.Errorf("nil HTTP response")
	}
	return resp.Body, resp.StatusCode, nil
}

// pickLyrics prefers synced (LRC) lyrics over plain text and returns "" for
// instrumental tracks or when no lyrics are present.
func pickLyrics(t *lrclibTrack) string {
	if t.Instrumental {
		return ""
	}
	if t.SyncedLyrics != "" {
		return t.SyncedLyrics
	}
	return t.PlainLyrics
}

// cacheKey derives a fixed-length KVStore key (the store caps keys at 256 bytes,
// so the normalized fields are hashed rather than concatenated).
func cacheKey(artist, title, album string, duration float32) string {
	h := fnv.New64a()
	write := func(s string) {
		_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(s))))
		_, _ = h.Write([]byte{0})
	}
	write(artist)
	write(title)
	write(album)
	_, _ = h.Write([]byte(strconv.Itoa(int(duration + 0.5))))
	return cachePrefix + strconv.FormatUint(h.Sum64(), 16)
}

// encodeCacheValue serializes a cache entry: 'H'+text for a hit, 'M' for a miss.
func encodeCacheValue(text string) []byte {
	if text == "" {
		return []byte{cacheValMiss}
	}
	return append([]byte{cacheValHit}, text...)
}

// decodeCacheValue parses a stored cache entry. ok=false for unrecognized bytes.
func decodeCacheValue(val []byte) (text string, miss bool, ok bool) {
	if len(val) == 0 {
		return "", false, false
	}
	switch val[0] {
	case cacheValMiss:
		return "", true, true
	case cacheValHit:
		return string(val[1:]), false, true
	default:
		return "", false, false
	}
}

// cacheGet returns (text, found, miss). found=false means "not cached"; miss=true
// means we previously recorded that LRCLIB has no lyrics for this track.
func cacheGet(key string) (string, bool, bool) {
	val, ok, err := host.KVStoreGet(key)
	if err != nil {
		pdk.Log(pdk.LogWarn, "lrclib: cache get failed: "+err.Error())
		return "", false, false
	}
	if !ok {
		return "", false, false
	}
	text, miss, decoded := decodeCacheValue(val)
	if !decoded {
		return "", false, false
	}
	return text, true, miss
}

// cacheStore records lyrics (or a miss) in the KVStore. Cache failures are
// non-fatal — lyrics were already fetched, so we just skip persisting.
func cacheStore(key, text string) {
	ttl := ttlHitSeconds
	if text == "" {
		ttl = ttlMissSeconds
	}
	if err := host.KVStoreSetWithTTL(key, encodeCacheValue(text), ttl); err != nil {
		pdk.Log(pdk.LogWarn, "lrclib: cache store failed: "+err.Error())
	}
}

func main() {}
