package skills

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Verify asks a fresh headless Claude Code session which skills it can see. The
// file check cannot catch a SKILL.md that is present but never loads (bad
// frontmatter, wrong folder name); this can. It starts its own session, so it
// does not depend on the session the developer is in.
func Verify(out io.Writer) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found; install Claude Code to run this check")
	}
	want, err := lockedSkillNames()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p",
		"List the names of every skill available to you, one per line, nothing else.")
	answer, err := cmd.Output()
	if ctx.Err() != nil {
		return fmt.Errorf("claude did not answer within 2 minutes")
	}
	if err != nil {
		return fmt.Errorf("claude -p: %w", err)
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(string(answer), "\n") {
		seen[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, name := range want {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("a fresh Claude Code session cannot see: %s\nit answered:\n%scheck the SKILL.md frontmatter, then: mise run dev:skills:sync",
			strings.Join(missing, ", "), indent(string(answer)))
	}

	fmt.Fprintf(out, "a fresh Claude Code session sees every skill in %s:\n", skillsDir)
	fmt.Fprint(out, indent(strings.Join(want, "\n")))
	return nil
}

// lockedSkillNames reads the skill names from SKILLS.lock.
func lockedSkillNames() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(skillsDir, lockFile))
	if err != nil {
		return nil, fmt.Errorf("%w; run: mise run dev:skills:sync", err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if name, _, ok := strings.Cut(line, "\t"); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s lists no skills", lockFile)
	}
	sort.Strings(names)
	return names, nil
}
