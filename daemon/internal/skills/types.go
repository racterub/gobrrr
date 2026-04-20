// Package skills loads SKILL.md files, maintains an in-memory registry, and
// builds the <available_skills> prompt block injected into worker prompts.
package skills

type SkillType string

const (
	TypeSystem  SkillType = "system"
	TypeClawhub SkillType = "clawhub"
	TypeUser    SkillType = "user"
)

// Skill is the loaded representation of one installed skill.
type Skill struct {
	Slug             string
	Description      string
	Path             string   // absolute path to SKILL.md
	Dir              string   // skill directory
	Type             SkillType
	ReadPermissions  []string // from _meta.json approved_read_permissions
	WritePermissions []string // from _meta.json approved_write_permissions
	Fingerprint      string   // sha256 of skill dir at last approval
}

// Frontmatter is the parsed YAML header of a SKILL.md.
type Frontmatter struct {
	Name        string     `yaml:"name" json:"name"`
	Description string     `yaml:"description" json:"description"`
	Metadata    FMMetadata `yaml:"metadata" json:"metadata"`
}

type FMMetadata struct {
	Gobrrr   FMGobrrr   `yaml:"gobrrr" json:"gobrrr"`
	OpenClaw FMOpenClaw `yaml:"openclaw" json:"openclaw"`
}

type FMGobrrr struct {
	Type string `yaml:"type" json:"type"` // system | clawhub | user
}

type FMOpenClaw struct {
	Emoji    string            `yaml:"emoji,omitempty" json:"emoji,omitempty"`
	Homepage string            `yaml:"homepage,omitempty" json:"homepage,omitempty"`
	Requires FMRequires        `yaml:"requires" json:"requires"`
	Install  []FMInstallRecipe `yaml:"install,omitempty" json:"install,omitempty"`
}

// FMInstallRecipe describes one way to install a missing binary on a host.
// A skill can list multiple recipes (e.g. brew + apt + dnf); the installer
// picks at most one per missing binary based on the host's package manager.
type FMInstallRecipe struct {
	ID      string   `yaml:"id" json:"id"`
	Kind    string   `yaml:"kind" json:"kind"`
	Formula string   `yaml:"formula,omitempty" json:"formula,omitempty"`
	Package string   `yaml:"package,omitempty" json:"package,omitempty"`
	Module  string   `yaml:"module,omitempty" json:"module,omitempty"`
	URL     string   `yaml:"url,omitempty" json:"url,omitempty"`
	Bins    []string `yaml:"bins,omitempty" json:"bins,omitempty"`
}

type FMRequires struct {
	Bins            []string         `yaml:"bins,omitempty" json:"bins,omitempty"`
	Env             []string         `yaml:"env,omitempty" json:"env,omitempty"`
	ToolPermissions FMToolPermissions `yaml:"tool_permissions" json:"tool_permissions"`
}

// FMToolPermissions supports both the split form
//
//	tool_permissions: { read: [...], write: [...] }
//
// and the legacy flat form
//
//	tool_permissions: [...]
//
// which is treated as all-read.
type FMToolPermissions struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}
