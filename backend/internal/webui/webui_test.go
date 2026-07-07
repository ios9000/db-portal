package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func spaFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         &fstest.MapFile{Data: []byte("<!doctype html><div id=\"root\">spa</div>")},
		"assets/app-4f2a.js": &fstest.MapFile{Data: []byte("console.log('app')")},
		"favicon.svg":        &fstest.MapFile{Data: []byte("<svg></svg>")},
	}
}

func do(t *testing.T, fsys fs.FS, method, target string) *http.Response {
	t.Helper()
	ts := httptest.NewServer(handler(fsys))
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(method, ts.URL+target, nil)
	require.NoError(t, err)
	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	return resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func TestRootServesIndex(t *testing.T) {
	resp := do(t, spaFS(), http.MethodGet, "/")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	require.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	require.Contains(t, body(t, resp), "spa")
}

func TestRealFilesServedAsIs(t *testing.T) {
	resp := do(t, spaFS(), http.MethodGet, "/assets/app-4f2a.js")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "console.log('app')", body(t, resp))
	require.Equal(t, "public, max-age=31536000, immutable", resp.Header.Get("Cache-Control"))

	resp = do(t, spaFS(), http.MethodGet, "/favicon.svg")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, resp.Header.Get("Cache-Control"), "only hashed assets/ cache forever")
}

func TestSPAFallbackForClientRoutes(t *testing.T) {
	for _, target := range []string{"/instances/42", "/activity", "/index.html"} {
		resp := do(t, spaFS(), http.MethodGet, target)
		require.Equal(t, http.StatusOK, resp.StatusCode, target)
		require.Contains(t, body(t, resp), "spa", target)
	}
}

func TestAPIMissStaysPlain404(t *testing.T) {
	for _, target := range []string{"/api", "/api/nope"} {
		resp := do(t, spaFS(), http.MethodGet, target)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, target)
		require.NotContains(t, body(t, resp), "<", target)
	}
}

func TestNonGetRejected(t *testing.T) {
	resp := do(t, spaFS(), http.MethodPost, "/instances/42")
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestPlaceholderWhenDistMissing(t *testing.T) {
	resp := do(t, fstest.MapFS{}, http.MethodGet, "/")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	require.Contains(t, body(t, resp), "build:release")
}

// TestEmbeddedHandler exercises the real embed. It must pass in both
// states of the tree: placeholder-only (fresh checkout) and with a real
// dist copied in by build:release — so it only asserts HTML comes back.
func TestEmbeddedHandler(t *testing.T) {
	ts := httptest.NewServer(Handler())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL + "/")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
}
