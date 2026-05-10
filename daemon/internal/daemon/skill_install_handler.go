package daemon

import (
	"encoding/json"
	"fmt"

	"github.com/racterub/gobrrr/internal/clawhub"
)

// committerLike is the minimal contract skillInstallHandler needs. Exists so
// tests can inject a fake.
type committerLike interface {
	Commit(req clawhub.InstallRequest, decision clawhub.Decision) error
}

// refresherLike is the minimal contract for reloading the skill registry after
// a successful install. *skills.Registry satisfies this directly.
type refresherLike interface {
	Refresh() error
}

// skillInstallHandler implements ApprovalHandler for kind="skill_install".
// It unmarshals the payload (a clawhub.InstallRequest) and delegates to the
// committer with a decision mapped from the generic string. After a successful
// approve commit, it triggers a registry refresh so newly-installed skills are
// visible to subsequent worker spawns without a daemon restart.
type skillInstallHandler struct {
	committer committerLike
	refresher refresherLike
}

// NewSkillInstallHandlerForTesting exposes the internal handler to other
// packages' tests. Production code registers via the dispatcher wiring in
// daemon.New.
func NewSkillInstallHandlerForTesting(c committerLike, r refresherLike) ApprovalHandler {
	return &skillInstallHandler{committer: c, refresher: r}
}

func (h *skillInstallHandler) Handle(req *ApprovalRequest, decision string) error {
	var installReq clawhub.InstallRequest
	if err := json.Unmarshal(req.Payload, &installReq); err != nil {
		return fmt.Errorf("skill_install: bad payload: %w", err)
	}
	var d clawhub.Decision
	switch decision {
	case "approve":
		d = clawhub.Decision{Approve: true}
	case "skip_binary":
		d = clawhub.Decision{Approve: true, SkipBinary: true}
	case "deny":
		d = clawhub.Decision{Approve: false}
	default:
		return fmt.Errorf("skill_install: unknown decision %q", decision)
	}
	return h.committer.Commit(installReq, d)
}
