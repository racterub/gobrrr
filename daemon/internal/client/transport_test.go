package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostJSON_OK(t *testing.T) {
	var gotBody, gotCT, gotTask string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		gotCT = r.Header.Get("Content-Type")
		gotTask = r.Header.Get("X-Gobrrr-Task-ID")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	raw, err := c.postJSON("/x", map[string]string{"k": "v"}, "task-123", http.StatusOK)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, string(raw))
	assert.Equal(t, "application/json", gotCT)
	assert.Equal(t, "task-123", gotTask)
	assert.Contains(t, gotBody, `"k":"v"`)
}

func TestPostJSON_NoTaskID_NoHeader(t *testing.T) {
	var gotTask string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTask = r.Header.Get("X-Gobrrr-Task-ID")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", struct{}{}, "", http.StatusNoContent)
	require.NoError(t, err)
	assert.Equal(t, "", gotTask)
}

func TestPostJSON_403_ReturnsErrWriteNotPermitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", struct{}{}, "task-1", http.StatusNoContent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWriteNotPermitted))
	assert.Equal(t, "write not permitted: task does not have allow_writes", err.Error())
}

func TestPostJSON_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", struct{}{}, "", http.StatusOK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "/x")
}

func TestPostJSON_NilBody(t *testing.T) {
	var gotLen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLen = r.ContentLength
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", nil, "", http.StatusOK)
	require.NoError(t, err)
	assert.Equal(t, int64(0), gotLen)
}

func TestGetJSON_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = io.WriteString(w, `[1,2,3]`)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	raw, err := c.getJSON("/y")
	require.NoError(t, err)
	assert.Equal(t, "[1,2,3]", strings.TrimSpace(string(raw)))
}

func TestGetJSON_404_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.getJSON("/y")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestGetJSON_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.getJSON("/y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestDeleteResource_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	err := c.deleteResource("/z", http.StatusNoContent)
	require.NoError(t, err)
}

func TestDeleteResource_404_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	err := c.deleteResource("/z", http.StatusNoContent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestDeleteResource_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	err := c.deleteResource("/z", http.StatusNoContent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}
