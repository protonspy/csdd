// Package workspace resolves and validates the Claude Code workspace root.
// A workspace is any directory containing a .claude/ subdirectory; the CLI
// walks upward from the working directory to locate it.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/protonspy/csdd/internal/paths"
)

var kebabRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// IncusionModes lists every valid steering inclusion frontmatter value.
var InclusionModes = []string{"always", "fileMatch", "manual", "auto"}

// SpecPhases enumerates the human-approved spec phases.
var SpecPhases = []string{"requirements", "design", "tasks"}

// SpecArtifacts enumerates every generable artifact under specs/<feature>/.
var SpecArtifacts = []string{"requirements", "design", "tasks", "research", "bugfix"}

// KebabCheck rejects names that are not kebab-case, returning a descriptive error.
func KebabCheck(name, kind string) error {
	if !kebabRe.MatchString(name) {
		return fmt.Errorf("%s name must be kebab-case (lowercase, hyphen-separated): got %q", kind, name)
	}
	return nil
}

// Find walks upward from start looking for the nearest .claude/ directory.
// Returns the start directory unchanged when no workspace is found.
func Find(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	cur := abs
	for {
		if info, err := os.Stat(paths.Claude(cur)); err == nil && info.IsDir() {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		cur = parent
	}
}

// Resolve normalizes the user-supplied --root flag. An empty arg falls back to Find(cwd).
// A non-empty arg must exist on disk, otherwise an error is returned.
func Resolve(arg string) (string, error) {
	if arg != "" {
		p, err := filepath.Abs(arg)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("--root path does not exist: %s", p)
		}
		return p, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return Find(cwd), nil
}

// ErrAlreadyExists signals a refusal to overwrite an existing artifact without --force.
var ErrAlreadyExists = errors.New("already exists")

// SteeringDir returns the steering directory under root, requiring that .claude/steering/ exists.
func SteeringDir(root string) (string, error) {
	d := paths.Steering(root)
	if info, err := os.Stat(d); err != nil || !info.IsDir() {
		return "", fmt.Errorf("no .claude/steering directory at %s. Run `csdd init` first", root)
	}
	return d, nil
}

// SpecsDir returns the repo-root specs/ directory.
func SpecsDir(root string) (string, error) {
	d := paths.Specs(root)
	if info, err := os.Stat(d); err != nil || !info.IsDir() {
		return "", fmt.Errorf("no specs directory at %s. Run `csdd init` first", root)
	}
	return d, nil
}

// SkillRoot returns the canonical Claude Code skills directory (.claude/skills/).
// It is a pure path resolver with no side effects: read operations (list, show)
// must not materialize directories just to look something up. The create flow
// is responsible for making the directory (via WriteFile/MkdirAll on its target).
func SkillRoot(root string) string {
	return paths.Skills(root)
}

// AgentRoot returns the canonical Claude Code custom-agents directory (.claude/agents/).
// Like SkillRoot, it is a pure path resolver with no side effects.
func AgentRoot(root string) string {
	return paths.Agents(root)
}

// SafeWrite writes content to path only when path is missing. Returns true when the file was created.
func SafeWrite(path, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

// WriteFile writes content unconditionally unless the path exists and overwrite is false.
func WriteFile(path, content string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s (pass --force)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// Relative reports the path of target relative to root (for friendly logging).
func Relative(root, target string) string {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return target
	}
	return rel
}
