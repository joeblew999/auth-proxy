package session

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
// Verify asks a fresh headless Claude Code session which skills it can see and
// holds the answer against SESSION.lock, which records every skill the session
// is allowed to have. Files cannot answer this: a SKILL.md can be present and
// never load, and a skill can load that no file here mentions — a marketplace
// plugin, or one synced from claude.ai, which no project setting can block.
//
// update rewrites the lock from what the session reports, for when a Claude
// Code upgrade adds a built-in.
func Verify(out io.Writer, update bool) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found; install Claude Code to run this check")
	}
	seen, answer, err := sessionSkills()
	if err != nil {
		return err
	}

	if update {
		if err := writeSessionLock(seen); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s now allows the %d skills this session reports:\n", sessionLockPath(), len(seen))
		fmt.Fprint(out, indent(strings.Join(seen, "\n")))
		return nil
	}

	// Vendored skills that never load are the older failure, and worth their
	// own message: the fix is frontmatter, not the lock.
	locked, err := lockedSkillNames()
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, name := range seen {
		present[name] = true
	}
	var missingSkills []string
	for _, name := range locked {
		if !present[name] {
			missingSkills = append(missingSkills, name)
		}
	}
	if len(missingSkills) > 0 {
		return fmt.Errorf("a fresh Claude Code session cannot see: %s\nit answered:\n%scheck the SKILL.md frontmatter, then: "+syncCmd,
			strings.Join(missingSkills, ", "), indent(answer))
	}

	allowed, err := sessionLock()
	if err != nil {
		return err
	}
	gone, arrived := diffSession(allowed, seen)
	if len(gone) > 0 {
		return fmt.Errorf("skills the lock allows are no longer in the session:\n%sif that is intended: %s --update",
			indent(strings.Join(gone, "\n")), verifyCmd())
	}
	if len(arrived) > 0 {
		return fmt.Errorf("skills reached this session that %s does not allow:\n%s%s",
			sessionLockPath(), indent(strings.Join(arrived, "\n")), arrivalAdvice(arrived))
	}

	fmt.Fprintf(out, "a fresh Claude Code session has exactly the %d skills %s allows:\n", len(seen), sessionLockPath())
	fmt.Fprint(out, indent(strings.Join(seen, "\n")))
	return nil
}

// sessionSkills asks a fresh session what it can see, returning the names it
// reported and its raw answer.
func sessionSkills() ([]string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p",
		"List the names of every skill available to you, one per line, nothing else.")
	answer, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, "", fmt.Errorf("claude did not answer within 2 minutes")
	}
	if err != nil {
		return nil, "", fmt.Errorf("claude -p: %w", err)
	}
	var names []string
	for _, line := range strings.Split(string(answer), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, string(answer), nil
}

// diffSession reports what the lock allows but the session lost, and what the
// session gained that the lock does not allow.
func diffSession(allowed, seen []string) (gone, arrived []string) {
	allow := map[string]bool{}
	for _, name := range allowed {
		allow[name] = true
	}
	have := map[string]bool{}
	for _, name := range seen {
		have[name] = true
	}
	for _, name := range allowed {
		if !have[name] {
			gone = append(gone, name)
		}
	}
	for _, name := range seen {
		if !allow[name] {
			arrived = append(arrived, name)
		}
	}
	return gone, arrived
}

// arrivalAdvice names the fix for what showed up. A namespaced name came from a
// marketplace plugin and can be blocked; anything else is either a Claude Code
// built-in after an upgrade, or a skill synced from claude.ai, which no project
// setting can block — only noticing it is possible.
func arrivalAdvice(arrived []string) string {
	plugins := map[string]bool{}
	for _, name := range arrived {
		if plugin, _, ok := strings.Cut(name, ":"); ok && plugin != "" {
			plugins[plugin] = true
		}
	}
	if len(plugins) > 0 {
		var names []string
		for plugin := range plugins {
			names = append(names, plugin)
		}
		sort.Strings(names)
		return fmt.Sprintf("these came from the %s plugin(s); add them to blocked_plugins in %s, then: %s",
			strings.Join(names, ", "), pinsFile, syncCmd)
	}
	return fmt.Sprintf("if these are meant to be here (a Claude Code upgrade, or a skill you enabled on claude.ai,\nwhich no project setting can block): %s --update", verifyCmd())
}

func verifyCmd() string { return strings.TrimSuffix(syncCmd, "sync") + "verify" }

// lockedSkillNames reads the skill names from SKILLS.lock.
func lockedSkillNames() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(skillsDir, lockFile))
	if err != nil {
		return nil, fmt.Errorf("%w; run: "+syncCmd, err)
	}
	// The lock has a row per skill and a row per file within it. Only the
	// skills are names a session can report, so anything with a slash is a
	// file row: asking a session to list cloudflare/references/kv/api.md as a
	// skill fails every time.
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if name, _, ok := strings.Cut(line, "\t"); ok && !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s lists no skills", lockFile)
	}
	sort.Strings(names)
	return names, nil
}

// sessionLockFile records every skill a session is allowed to have: the ones
// this repo provides, plus whatever Claude Code ships. It lives beside the
// skills so one directory holds everything the session is pinned to.
const sessionLockFile = "SESSION.lock"

func sessionLockPath() string { return filepath.Join(skillsDir, sessionLockFile) }

// sessionLock reads the allowed set. Its absence is an error naming the fix,
// because an empty set would silently allow everything.
func sessionLock() ([]string, error) {
	data, err := os.ReadFile(sessionLockPath())
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s does not exist yet; record what this session is allowed with: %s --update",
			sessionLockPath(), verifyCmd())
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if name := strings.TrimSpace(line); name != "" && !strings.HasPrefix(name, "#") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func writeSessionLock(names []string) error {
	body := "# Every skill a session here is allowed to have, recorded by\n" +
		"# `" + verifyCmd() + " --update`. Verify fails on anything else,\n" +
		"# including skills synced from claude.ai that no setting can block.\n" +
		strings.Join(names, "\n") + "\n"
	return os.WriteFile(sessionLockPath(), []byte(body), 0o644)
}
