package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/racterub/gobrrr/internal/clawhub"
)

var validSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)

func (d *Daemon) registerSkillRoutes() {
	d.mux.HandleFunc("GET /skills/search", d.handleSkillsSearch)
	d.mux.HandleFunc("POST /skills/install", d.handleSkillsInstall)
	d.mux.HandleFunc("POST /skills/approve/{id}", d.handleSkillsApprove)
	d.mux.HandleFunc("POST /skills/deny/{id}", d.handleSkillsDeny)
	d.mux.HandleFunc("DELETE /skills/{slug}", d.handleSkillsUninstall)
}

func (d *Daemon) handleSkillsSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}
	results, err := d.clawhub.Search(q, 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeSkillJSON(w, results)
}

type installReq struct {
	Slug    string `json:"slug"`
	Version string `json:"version,omitempty"`
}

func (d *Daemon) handleSkillsInstall(w http.ResponseWriter, r *http.Request) {
	var body installReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Slug == "" {
		http.Error(w, "missing slug", http.StatusBadRequest)
		return
	}
	pkg, err := d.clawhub.Fetch(body.Slug, body.Version)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	reqID, err := d.installer.Stage(pkg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reqPath := filepath.Join(d.skillsRoot, "_requests", reqID+".json")
	data, err := os.ReadFile(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var req clawhub.InstallRequest
	if err := json.Unmarshal(data, &req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeSkillJSON(w, map[string]any{
		"request_id": reqID,
		"request":    req,
	})
}

type approveReq struct {
	SkipBinary bool `json:"skip_binary,omitempty"`
}

func (d *Daemon) handleSkillsApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body approveReq
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := d.committer.Commit(id, clawhub.Decision{Approve: true, SkipBinary: body.SkipBinary}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := d.skillReg.Refresh(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "approved")
}

func (d *Daemon) handleSkillsDeny(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.committer.Commit(id, clawhub.Decision{Approve: false}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "denied")
}

func (d *Daemon) handleSkillsUninstall(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.Error(w, "missing slug", http.StatusBadRequest)
		return
	}
	if !validSlug.MatchString(slug) {
		http.Error(w, "invalid slug", http.StatusBadRequest)
		return
	}
	dir := filepath.Join(d.skillsRoot, slug)
	if err := os.RemoveAll(dir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := d.skillReg.Refresh(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "uninstalled %s\n", slug)
}

func writeSkillJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
