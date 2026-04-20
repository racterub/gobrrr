package clawhub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Search(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "github", r.URL.Query().Get("q"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[
			{"score":3.7,"slug":"github","displayName":"Github","summary":"gh CLI ops","version":null,"updatedAt":1774865646622}
		]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	results, err := c.Search("github", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "github", results[0].Slug)
	assert.Equal(t, "Github", results[0].DisplayName)
	require.NotNil(t, results[0].Summary)
	assert.Equal(t, "gh CLI ops", *results[0].Summary)
	assert.Nil(t, results[0].Version, "search responses always return null version")
}

func TestClient_FetchAndVerifyBundle(t *testing.T) {
	zipBytes := []byte("fake zip bundle content, treat as opaque")
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/skills/github", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"skill":{"slug":"github","displayName":"Github","summary":"gh ops","tags":{"latest":"1.0.0"}},
			"latestVersion":{"version":"1.0.0","createdAt":0,"changelog":"","license":null}
		}`)
	})
	mux.HandleFunc("/api/v1/skills/github/versions/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"skill":{"slug":"github","displayName":"Github"},
			"version":{"version":"1.0.0","createdAt":0,"changelog":"","license":null,"files":[],
				"security":{"status":"clean","hasWarnings":false,"sha256hash":"%s"}
			}
		}`, hexSum)
	})
	mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "github", r.URL.Query().Get("slug"))
		assert.Equal(t, "1.0.0", r.URL.Query().Get("version"))
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pkg, err := c.Fetch("github", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "github", pkg.Slug)
	assert.Equal(t, "1.0.0", pkg.Version)
	assert.Equal(t, hexSum, pkg.SHA256)
	assert.Equal(t, zipBytes, pkg.BundleBytes)
}

func TestClient_FetchResolvesLatestWhenVersionEmpty(t *testing.T) {
	zipBytes := []byte("latest zip bytes")
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/skills/github", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"skill":{"slug":"github","tags":{"latest":"2.1.0"}},"latestVersion":{"version":"2.1.0"}}`)
	})
	mux.HandleFunc("/api/v1/skills/github/versions/2.1.0", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":{"version":"2.1.0","security":{"status":"clean","sha256hash":"%s"}}}`, hexSum)
	})
	mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2.1.0", r.URL.Query().Get("version"),
			"client must resolve latest version before calling download")
		_, _ = w.Write(zipBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pkg, err := c.Fetch("github", "")
	require.NoError(t, err)
	assert.Equal(t, "2.1.0", pkg.Version)
}

func TestClient_BundleChecksumMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/skills/github", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"skill":{"slug":"github"},"latestVersion":{"version":"1.0.0"}}`)
	})
	mux.HandleFunc("/api/v1/skills/github/versions/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":{"version":"1.0.0","security":{"sha256hash":"deadbeef"}}}`)
	})
	mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("definitely not matching deadbeef"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Fetch("github", "1.0.0")
	require.Error(t, err)
	assert.ErrorContains(t, err, "checksum mismatch")
}

func TestClient_SearchNon200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := NewClient(srv.URL).Search("x", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestClient_FetchFallsBackToTagsLatest(t *testing.T) {
	zipBytes := []byte("fallback via tags.latest")
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	// latestVersion omitted (decodes to nil); tags.latest carries the version.
	mux.HandleFunc("/api/v1/skills/github", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"skill":{"slug":"github","tags":{"latest":"3.0.0"}}}`)
	})
	mux.HandleFunc("/api/v1/skills/github/versions/3.0.0", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":{"version":"3.0.0","security":{"sha256hash":"%s"}}}`, hexSum)
	})
	mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "3.0.0", r.URL.Query().Get("version"))
		_, _ = w.Write(zipBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pkg, err := c.Fetch("github", "")
	require.NoError(t, err)
	assert.Equal(t, "3.0.0", pkg.Version)
}
