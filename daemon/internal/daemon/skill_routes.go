package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/racterub/gobrrr/internal/skills"
)

var validSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)

func (d *Daemon) registerSkillRoutes() {
	d.mux.HandleFunc("GET /skills", d.handleListSkills)
	d.mux.HandleFunc("GET /skills/search", d.handleSkillsSearch)
	d.mux.HandleFunc("POST /skills/install", d.handleSkillsInstall)
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
	installReq, err := d.installer.Stage(pkg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	approval, err := d.approvals.Create(
		"skill_install",
		"install skill "+installReq.Slug+"@"+installReq.Version,
		"",
		[]string{"approve", "skip_binary", "deny"},
		installReq,
		24*time.Hour,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeSkillJSON(w, map[string]any{
		"request_id": approval.ID,
		"request":    installReq, // CLI card still expects this shape
	})
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

func (d *Daemon) handleListSkills(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if d.skillReg == nil {
		_ = json.NewEncoder(w).Encode([]skills.Skill{})
		return
	}
	list := d.skillReg.List()
	_ = json.NewEncoder(w).Encode(list)
}
