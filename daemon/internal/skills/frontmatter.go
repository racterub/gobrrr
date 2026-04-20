package skills

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

var (
	errNoFrontmatter  = errors.New("skills: no YAML frontmatter found")
	errBadFrontmatter = errors.New("skills: malformed YAML frontmatter")
)

// ParseFrontmatter extracts and parses the YAML frontmatter from a SKILL.md
// file's bytes. It returns the parsed Frontmatter, the body after the
// closing "---", and an error if frontmatter is missing or malformed.
func ParseFrontmatter(content []byte) (*Frontmatter, []byte, error) {
	const delim = "---"
	if !bytes.HasPrefix(content, []byte(delim+"\n")) && !bytes.HasPrefix(content, []byte(delim+"\r\n")) {
		return nil, nil, errNoFrontmatter
	}
	after := content[len(delim):]
	if len(after) > 0 && after[0] == '\r' {
		after = after[1:]
	}
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	}
	end := bytes.Index(after, []byte("\n"+delim))
	if end < 0 {
		return nil, nil, errNoFrontmatter
	}
	yamlBytes := after[:end]
	body := after[end+len("\n"+delim):]
	if len(body) > 0 && body[0] == '\r' {
		body = body[1:]
	}
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(yamlBytes, &fm); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errBadFrontmatter, err)
	}
	return &fm, body, nil
}

// UnmarshalYAML accepts both
//
//	tool_permissions: { read: [...], write: [...] }
//
// and the legacy flat form
//
//	tool_permissions: [...]
//
// which is treated as all-read.
func (tp *FMToolPermissions) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var flat []string
		if err := node.Decode(&flat); err != nil {
			return err
		}
		tp.Read = flat
		tp.Write = nil
		return nil
	case yaml.MappingNode:
		var split struct {
			Read  []string `yaml:"read"`
			Write []string `yaml:"write"`
		}
		if err := node.Decode(&split); err != nil {
			return err
		}
		tp.Read = split.Read
		tp.Write = split.Write
		return nil
	default:
		return fmt.Errorf("tool_permissions: expected sequence or mapping, got %v", node.Kind)
	}
}
