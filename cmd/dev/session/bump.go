package session

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Bump moves every github source's pin in session.toml to upstream HEAD. It
// shows the file-level diff first and waits for confirmation, so moving forward
// is one deliberate command instead of a silent drift. Usage:
// dev skills bump [source] — one source, or all github sources when omitted.
func Bump(out io.Writer, sources []string) error {
	p, err := loadPins()
	if err != nil {
		return err
	}
	names := p.names()
	if len(sources) > 0 {
		for _, name := range sources {
			if _, ok := p.Source[name]; !ok {
				return fmt.Errorf("%s has no [source.%s]", pinsFile, name)
			}
		}
		names = sources
	}
	github := names

	oldFiles, err := pinnedSkills()
	if err != nil {
		return err
	}
	moved := false
	for _, name := range github {
		s := p.Source[name]
		head, err := lsRemoteHead(s.Repo)
		if err != nil {
			return err
		}
		if head == s.Ref {
			fmt.Fprintf(out, "%s is already at upstream HEAD (%s)\n", s.Repo, shortRef(head))
			continue
		}
		fmt.Fprintf(out, "%s: %s -> %s\n", s.Repo, shortRef(s.Ref), shortRef(head))
		s.Ref = head
		p.Source[name] = s
		moved = true
	}
	if !moved {
		return nil
	}
	newFiles, err := pinnedSkills()
	if err != nil {
		return err
	}
	for _, line := range diffFiles(oldFiles, newFiles) {
		if strings.HasPrefix(line, "missing: "+lockFile) || strings.HasPrefix(line, "changed: "+lockFile) {
			continue
		}
		fmt.Fprintln(out, "  "+line)
	}

	if !confirm(out, "rewrite "+pinsFile+"? [y/N] ") {
		return fmt.Errorf("not bumped")
	}
	if err := rewriteRefs(pinsFile, p); err != nil {
		return err
	}
	fmt.Fprintf(out, "pinned; run: %s\n", syncCmd)
	return nil
}

// lsRemoteHead returns upstream HEAD without cloning.
func lsRemoteHead(repo string) (string, error) {
	out, err := exec.Command("git", "ls-remote", "https://github.com/"+repo, "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote https://github.com/%s HEAD: %w", repo, err)
	}
	head, _, ok := strings.Cut(string(out), "\t")
	if !ok || head == "" {
		return "", fmt.Errorf("git ls-remote https://github.com/%s HEAD answered %q", repo, strings.TrimSpace(string(out)))
	}
	return head, nil
}

func shortRef(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

func confirm(out io.Writer, prompt string) bool {
	fmt.Fprint(out, prompt)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// rewriteRefs replaces the ref line of every github source in session.toml,
// keeping comments and order.
func rewriteRefs(path string, p pins) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out []string
	var section string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.Trim(trimmed, "[]")
		}
		if name, ok := strings.CutPrefix(section, "source."); ok {
			if s, known := p.Source[name]; known && strings.HasPrefix(trimmed, "ref") {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				line = indent + `ref = "` + s.Ref + `"`
			}
		}
		out = append(out, line)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
