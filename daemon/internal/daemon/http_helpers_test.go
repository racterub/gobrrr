package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON_Valid(t *testing.T) {
	body := strings.NewReader(`{"name":"x","count":3}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)

	var dst struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := decodeJSON(r, &dst); err != nil {
		t.Fatalf("decodeJSON returned error: %v", err)
	}
	if dst.Name != "x" || dst.Count != 3 {
		t.Fatalf("decoded value mismatch: %+v", dst)
	}
}

func TestDecodeJSON_Malformed(t *testing.T) {
	body := strings.NewReader(`{not json`)
	r := httptest.NewRequest(http.MethodPost, "/", body)

	var dst struct{}
	if err := decodeJSON(r, &dst); err == nil {
		t.Fatal("decodeJSON should have returned an error for malformed JSON")
	}
}

func TestRespondJSON_WritesHeaderAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"hello": "world"}

	respondJSON(rec, http.StatusCreated, payload)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if got["hello"] != "world" {
		t.Fatalf("body = %v, want hello=world", got)
	}
}

func TestRespondError_WritesEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()

	respondError(rec, http.StatusBadRequest, "missing prompt")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if got["error"] != "missing prompt" {
		t.Fatalf("body = %v, want error=missing prompt", got)
	}
}

func TestRespondError_EscapesQuotesInMessage(t *testing.T) {
	// Regression: routes_schedule.go used fmt.Sprintf with %q to handle
	// quotes in scheduler errors. respondError must preserve that safety.
	rec := httptest.NewRecorder()

	respondError(rec, http.StatusBadRequest, `bad "name" value`)

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if got["error"] != `bad "name" value` {
		t.Fatalf("body = %v, want error=%q", got, `bad "name" value`)
	}
}

// Ensure helpers don't break when given a nil body — used by some
// handlers that respond with a sentinel object without a request body.
func TestRespondJSON_NilSafe(t *testing.T) {
	rec := httptest.NewRecorder()
	respondJSON(rec, http.StatusOK, struct{}{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("{}")) {
		t.Fatalf("body = %q, want {} prefix", rec.Body.String())
	}
}
