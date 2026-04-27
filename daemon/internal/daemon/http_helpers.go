package daemon

import (
	"encoding/json"
	"net/http"
)

// decodeJSON decodes the request body JSON into dst. The caller decides how
// to respond on error (some endpoints use the JSON envelope, others use
// plain text), so this only forwards the json.Decoder error.
func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

// respondJSON writes a status line, sets Content-Type to application/json,
// and encodes v as the response body. Encode errors are intentionally
// swallowed — the response is already committed once WriteHeader is called.
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// respondError writes a JSON error envelope of the form {"error":"<msg>"}.
// Used by handlers that already speak the JSON-error shape; plain-text
// handlers (skill, approvals) keep their own http.Error calls.
func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
