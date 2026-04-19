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
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Metadata    FMMetadata `yaml:"metadata"`
}

type FMMetadata struct {
	Gobrrr   FMGobrrr   `yaml:"gobrrr"`
	OpenClaw FMOpenClaw `yaml:"openclaw"`
}

type FMGobrrr struct {
	Type string `yaml:"type"` // system | clawhub | user
}

type FMOpenClaw struct {
	Emoji    string     `yaml:"emoji,omitempty"`
	Homepage string     `yaml:"homepage,omitempty"`
	Requires FMRequires `yaml:"requires"`
}

type FMRequires struct {
	Bins            []string         `yaml:"bins,omitempty"`
	Env             []string         `yaml:"env,omitempty"`
	ToolPermissions FMToolPermissions `yaml:"tool_permissions"`
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
	Read  []string
	Write []string
}
