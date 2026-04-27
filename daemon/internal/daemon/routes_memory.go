package daemon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/racterub/gobrrr/internal/memory"
)

type saveMemoryRequest struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Source  string   `json:"source"`
}

func (d *Daemon) handleSaveMemory(w http.ResponseWriter, r *http.Request) {
	var req saveMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, `{"error":"content is required"}`, http.StatusBadRequest)
		return
	}

	entry, err := d.memStore.Save(req.Content, req.Tags, req.Source)
	if err != nil {
		http.Error(w, `{"error":"failed to save memory"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(entry) //nolint:errcheck
}

func (d *Daemon) handleSearchMemory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	tagsParam := r.URL.Query().Get("tags")
	limitParam := r.URL.Query().Get("limit")

	var tags []string
	if tagsParam != "" {
		for _, t := range strings.Split(tagsParam, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	limit := 0
	if limitParam != "" {
		if n, err := strconv.Atoi(limitParam); err == nil && n > 0 {
			limit = n
		}
	}

	var entries []*memory.Entry
	var err error
	if q != "" || len(tags) > 0 {
		entries, err = d.memStore.Search(q, tags, limit)
	} else {
		entries, err = d.memStore.List(limit)
	}
	if err != nil {
		http.Error(w, `{"error":"failed to search memory"}`, http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []*memory.Entry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries) //nolint:errcheck
}

func (d *Daemon) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, "/\\") {
		http.Error(w, `{"error":"invalid memory id"}`, http.StatusBadRequest)
		return
	}

	entry, err := d.memStore.Get(id)
	if err != nil {
		http.Error(w, `{"error":"memory not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry) //nolint:errcheck
}

func (d *Daemon) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, "/\\") {
		http.Error(w, `{"error":"invalid memory id"}`, http.StatusBadRequest)
		return
	}

	if err := d.memStore.Delete(id); err != nil {
		http.Error(w, `{"error":"failed to delete memory"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
