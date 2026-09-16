package main

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
		return fmt.Errorf("a fresh Claude Code session cannot see: %s\nit answered:\n%scheck the SKILL.md frontmatter, then: "+syncCmd,
			strings.Join(missing, ", "), indent(string(answer)))
	}
	// The session can also see skills this repo does not pin. A plugin one
	// named <plugin>:<pinned name> is a second copy of a pinned skill at a
	// version skills.toml does not control, which is how the two drifted
	// apart before [claude] existed.
	if shadows := shadowed(seen, want); len(shadows) > 0 {
		return fmt.Errorf("these also provide a skill pinned in %s:\n%sadd the plugin to blocked_plugins in %s, then: "+syncCmd,
			skillsDir, indent(strings.Join(shadows, "\n")), pinsFile)
	}

	fmt.Fprintf(out, "a fresh Claude Code session sees every skill in %s:\n", skillsDir)
	fmt.Fprint(out, indent(strings.Join(want, "\n")))
	return nil
}

// shadowed returns the namespaced skills the session saw that duplicate a
// pinned name, as "<plugin>:<skill> shadows <skill>".
func shadowed(seen map[string]bool, want []string) []string {
	pinned := map[string]bool{}
	for _, name := range want {
		pinned[name] = true
	}
	var shadows []string
	for name := range seen {
		plugin, skill, ok := strings.Cut(name, ":")
		if ok && plugin != "" && pinned[skill] {
			shadows = append(shadows, fmt.Sprintf("%s shadows %s", name, skill))
		}
	}
	sort.Strings(shadows)
	return shadows
}

// lockedSkillNames reads the skill names from SKILLS.lock.
func lockedSkillNames() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(skillsDir, lockFile))
	if err != nil {
		return nil, fmt.Errorf("%w; run: "+syncCmd, err)
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
