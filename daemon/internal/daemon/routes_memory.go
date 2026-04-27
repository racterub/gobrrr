package daemon

import (
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
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Content == "" {
		respondError(w, http.StatusBadRequest, "content is required")
		return
	}

	entry, err := d.memStore.Save(req.Content, req.Tags, req.Source)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save memory")
		return
	}

	respondJSON(w, http.StatusCreated, entry)
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
		respondError(w, http.StatusInternalServerError, "failed to search memory")
		return
	}
	if entries == nil {
		entries = []*memory.Entry{}
	}

	respondJSON(w, http.StatusOK, entries)
}

func (d *Daemon) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, "/\\") {
		respondError(w, http.StatusBadRequest, "invalid memory id")
		return
	}

	entry, err := d.memStore.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "memory not found")
		return
	}

	respondJSON(w, http.StatusOK, entry)
}

func (d *Daemon) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, "/\\") {
		respondError(w, http.StatusBadRequest, "invalid memory id")
		return
	}

	if err := d.memStore.Delete(id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete memory")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
