package main

import (
	"strings"
	"testing"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/lyrics"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// resetMocks clears the package-level PDK mocks between tests and installs a
// permissive Log expectation so error-path logging never panics.
func resetMocks() {
	host.HTTPMock.ExpectedCalls = nil
	host.HTTPMock.Calls = nil
	host.KVStoreMock.ExpectedCalls = nil
	host.KVStoreMock.Calls = nil
	pdk.PDKMock.ExpectedCalls = nil
	pdk.PDKMock.Calls = nil
	pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Return()
}

// urlContains matches an HTTPRequest whose URL contains the given fragment.
func urlContains(fragment string) any {
	return mock.MatchedBy(func(r host.HTTPRequest) bool {
		return strings.Contains(r.URL, fragment)
	})
}

func httpOK(body string) (*host.HTTPResponse, error) {
	return &host.HTTPResponse{StatusCode: 200, Body: []byte(body)}, nil
}

func httpStatus(code int32) (*host.HTTPResponse, error) {
	return &host.HTTPResponse{StatusCode: code}, nil
}

func cacheMissGet() {
	host.KVStoreMock.On("Get", mock.Anything).Return([]byte(nil), false, nil)
}

func req(title, artist, album string, duration float32) lyrics.GetLyricsRequest {
	return lyrics.GetLyricsRequest{Track: lyrics.TrackInfo{
		Title: title, Artist: artist, Album: album, Duration: duration,
	}}
}

// --- Pure helpers -----------------------------------------------------------

func TestPickLyrics(t *testing.T) {
	tests := []struct {
		name string
		in   lrclibTrack
		want string
	}{
		{"synced preferred", lrclibTrack{SyncedLyrics: "[00:01.00] a", PlainLyrics: "a"}, "[00:01.00] a"},
		{"plain fallback", lrclibTrack{PlainLyrics: "just plain"}, "just plain"},
		{"instrumental empty", lrclibTrack{Instrumental: true, SyncedLyrics: "[00:01.00] x"}, ""},
		{"none empty", lrclibTrack{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pickLyrics(&tc.in))
		})
	}
}

func TestCacheKey(t *testing.T) {
	base := cacheKey("Radiohead", "Creep", "Pablo Honey", 238)

	// Deterministic + normalized (case/whitespace insensitive).
	assert.Equal(t, base, cacheKey("  radiohead ", "CREEP", "pablo honey ", 238))
	// Rounds duration to the nearest second.
	assert.Equal(t, base, cacheKey("Radiohead", "Creep", "Pablo Honey", 238.4))

	// Distinct inputs produce distinct keys.
	assert.NotEqual(t, base, cacheKey("Radiohead", "Creep", "Pablo Honey", 240))
	assert.NotEqual(t, base, cacheKey("Muse", "Creep", "Pablo Honey", 238))

	// Bounded length (KVStore caps keys at 256 bytes) and stable prefix.
	assert.True(t, strings.HasPrefix(base, cachePrefix))
	long := cacheKey(strings.Repeat("x", 5000), strings.Repeat("y", 5000), strings.Repeat("z", 5000), 200)
	assert.LessOrEqual(t, len(long), 256)
}

func TestEncodeDecodeCacheValue(t *testing.T) {
	// Hit round-trips verbatim.
	text, miss, ok := decodeCacheValue(encodeCacheValue("[00:01.00] hello"))
	assert.True(t, ok)
	assert.False(t, miss)
	assert.Equal(t, "[00:01.00] hello", text)

	// Miss round-trips as a miss.
	text, miss, ok = decodeCacheValue(encodeCacheValue(""))
	assert.True(t, ok)
	assert.True(t, miss)
	assert.Equal(t, "", text)

	// Empty / unrecognized bytes are not usable cache entries.
	_, _, ok = decodeCacheValue(nil)
	assert.False(t, ok)
	_, _, ok = decodeCacheValue([]byte{'?'})
	assert.False(t, ok)
}

// --- GetLyrics integration (mocked host) ------------------------------------

func TestGetLyrics_CacheHit(t *testing.T) {
	resetMocks()
	p := &lrclibPlugin{}
	cached := encodeCacheValue("[00:01.00] cached line")
	host.KVStoreMock.On("Get", mock.Anything).Return(cached, true, nil)

	resp, err := p.GetLyrics(req("Creep", "Radiohead", "Pablo Honey", 238))

	assert.NoError(t, err)
	assert.Len(t, resp.Lyrics, 1)
	assert.Equal(t, "[00:01.00] cached line", resp.Lyrics[0].Text)
	// A cache hit must NOT touch the network or write back.
	host.HTTPMock.AssertNotCalled(t, "Send", mock.Anything)
	host.KVStoreMock.AssertNotCalled(t, "SetWithTTL", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetLyrics_CachedMiss(t *testing.T) {
	resetMocks()
	p := &lrclibPlugin{}
	host.KVStoreMock.On("Get", mock.Anything).Return(encodeCacheValue(""), true, nil)

	resp, err := p.GetLyrics(req("Creep", "Radiohead", "Pablo Honey", 238))

	assert.NoError(t, err)
	assert.Empty(t, resp.Lyrics)
	host.HTTPMock.AssertNotCalled(t, "Send", mock.Anything)
}

func TestGetLyrics_FreshFetchCachesHit(t *testing.T) {
	resetMocks()
	p := &lrclibPlugin{}
	cacheMissGet()
	body := `{"trackName":"Creep","instrumental":false,"plainLyrics":"plain","syncedLyrics":"[00:01.00] synced"}`
	r, e := httpOK(body)
	host.HTTPMock.On("Send", urlContains("/api/get")).Return(r, e)
	host.KVStoreMock.On("SetWithTTL", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	resp, err := p.GetLyrics(req("Creep", "Radiohead", "Pablo Honey", 238))

	assert.NoError(t, err)
	assert.Len(t, resp.Lyrics, 1)
	assert.Equal(t, "[00:01.00] synced", resp.Lyrics[0].Text) // synced preferred over plain
	// Result is cached as a hit with the long TTL.
	key := cacheKey("Radiohead", "Creep", "Pablo Honey", 238)
	host.KVStoreMock.AssertCalled(t, "SetWithTTL", key, encodeCacheValue("[00:01.00] synced"), ttlHitSeconds)
}

func TestGetLyrics_FallbackToSearch(t *testing.T) {
	resetMocks()
	p := &lrclibPlugin{}
	cacheMissGet()
	get404, e1 := httpStatus(404)
	host.HTTPMock.On("Send", urlContains("/api/get")).Return(get404, e1)
	searchBody := `[{"trackName":"Creep","plainLyrics":"plain only","syncedLyrics":""}]`
	r, e2 := httpOK(searchBody)
	host.HTTPMock.On("Send", urlContains("/api/search")).Return(r, e2)
	host.KVStoreMock.On("SetWithTTL", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	resp, err := p.GetLyrics(req("Creep", "Radiohead", "Pablo Honey", 238))

	assert.NoError(t, err)
	assert.Len(t, resp.Lyrics, 1)
	assert.Equal(t, "plain only", resp.Lyrics[0].Text)
	host.HTTPMock.AssertCalled(t, "Send", urlContains("/api/search"))
}

func TestGetLyrics_NoLyricsCachesMiss(t *testing.T) {
	resetMocks()
	p := &lrclibPlugin{}
	cacheMissGet()
	get404, e1 := httpStatus(404)
	host.HTTPMock.On("Send", urlContains("/api/get")).Return(get404, e1)
	empty, e2 := httpOK(`[]`)
	host.HTTPMock.On("Send", urlContains("/api/search")).Return(empty, e2)
	host.KVStoreMock.On("SetWithTTL", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	resp, err := p.GetLyrics(req("Unknown Song", "Nobody", "", 100))

	assert.NoError(t, err)
	assert.Empty(t, resp.Lyrics)
	// A miss is cached (with the shorter TTL) so we don't re-hit LRCLIB next time.
	key := cacheKey("Nobody", "Unknown Song", "", 100)
	host.KVStoreMock.AssertCalled(t, "SetWithTTL", key, encodeCacheValue(""), ttlMissSeconds)
}

func TestGetLyrics_Instrumental(t *testing.T) {
	resetMocks()
	p := &lrclibPlugin{}
	cacheMissGet()
	body := `{"trackName":"Jessica","instrumental":true,"plainLyrics":"","syncedLyrics":""}`
	r, e := httpOK(body)
	host.HTTPMock.On("Send", urlContains("/api/get")).Return(r, e)
	host.KVStoreMock.On("SetWithTTL", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	resp, err := p.GetLyrics(req("Jessica", "The Allman Brothers Band", "Brothers and Sisters", 456))

	assert.NoError(t, err)
	assert.Empty(t, resp.Lyrics)
}

func TestGetLyrics_EmptyTitle(t *testing.T) {
	resetMocks()
	p := &lrclibPlugin{}

	resp, err := p.GetLyrics(req("", "Radiohead", "Pablo Honey", 238))

	assert.NoError(t, err)
	assert.Empty(t, resp.Lyrics)
	// No title -> nothing to look up, no cache or network access.
	host.KVStoreMock.AssertNotCalled(t, "Get", mock.Anything)
	host.HTTPMock.AssertNotCalled(t, "Send", mock.Anything)
}
