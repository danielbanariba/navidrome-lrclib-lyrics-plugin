// Navidrome LRCLIB lyrics plugin.
//
// Implements the Navidrome Lyrics capability by fetching synced (LRC) or plain
// lyrics from the LRCLIB public API (https://lrclib.net). The plugin returns the
// raw lyrics string verbatim; Navidrome parses LRC timestamps host-side
// (model.ToLyrics), so the plugin must NOT build synced-line structures itself.
//
// Build (see Makefile):
//
//	make                 # produces lrclib.ndp
//
// or manually:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
//	zip -j lrclib.ndp manifest.json plugin.wasm
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/lyrics"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const (
	apiBase       = "https://lrclib.net/api"
	userAgent     = "navidrome-lrclib-lyrics-plugin/0.1.0 (https://github.com/danielbanariba/navidrome-lrclib-lyrics-plugin)"
	httpTimeoutMs = 10000
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

// GetLyrics looks up lyrics for the given track on LRCLIB. It first tries an
// exact /api/get match (artist + track + album + duration); if that misses, it
// falls back to /api/search by track + artist. An empty response tells Navidrome
// to move on to the next lyrics source.
func (p *lrclibPlugin) GetLyrics(req lyrics.GetLyricsRequest) (lyrics.GetLyricsResponse, error) {
	t := req.Track

	artist := t.Artist
	if artist == "" && len(t.Artists) > 0 {
		artist = t.Artists[0].Name
	}

	track := p.lookup(t.Title, artist, t.Album, t.Duration)
	if track == nil {
		return lyrics.GetLyricsResponse{}, nil
	}

	text := pickLyrics(track)
	if text == "" {
		return lyrics.GetLyricsResponse{}, nil
	}

	// Raw text (plain or LRC). LRCLIB does not expose a language, so Lang is left
	// empty; the host defaults it to "xxx".
	return lyrics.GetLyricsResponse{
		Lyrics: []lyrics.LyricsText{{Text: text}},
	}, nil
}

// lookup tries the exact endpoint first, then the search fallback.
func (p *lrclibPlugin) lookup(title, artist, album string, duration float32) *lrclibTrack {
	if title == "" {
		return nil
	}
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

func main() {}
