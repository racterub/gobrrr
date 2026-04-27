package daemon

import (
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
	respondJSON(w, http.StatusOK, results)
}

type installReq struct {
	Slug    string `json:"slug"`
	Version string `json:"version,omitempty"`
}

func (d *Daemon) handleSkillsInstall(w http.ResponseWriter, r *http.Request) {
	var body installReq
	if err := decodeJSON(r, &body); err != nil {
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
	respondJSON(w, http.StatusOK, map[string]any{
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

func (d *Daemon) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if d.skillReg == nil {
		respondJSON(w, http.StatusOK, []skills.Skill{})
		return
	}
	list := d.skillReg.List()
	respondJSON(w, http.StatusOK, list)
}
