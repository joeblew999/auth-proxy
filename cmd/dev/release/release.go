// Package release publishes a GitHub Release fully locally: goreleaser builds the
// artifacts, packslip signs the manifest, gh uploads everything. No workflow.
//
// It runs as `dev release ...` through cmd/dev. The binary name and the skill
// resource come from flags, the repo slug from the origin remote.
package release

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const usage = `dev release: publish a GitHub Release fully locally (goreleaser + packslip + gh).

  dev release snapshot
      build release artifacts locally without publishing
  dev release packslip --bin <name> [--resource <spec>]...
      build the packslip manifest for the snapshot artifacts and verify it
  dev release publish --bin <name> [--resource <spec>]... [--version <vX.Y.Z>]
      tag, build, sign, upload (CI uses GITHUB_REF_NAME instead of --version)

Run from the repo root. Needs .goreleaser.yml, goreleaser, packslip and gh.
`

// Run dispatches the release subcommand. Args are everything after "release".
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageMessage(nil))
		return errUsage(nil)
	}
	switch args[0] {
	case "snapshot":
		if err := requireNoArgs(args[1:]); err != nil {
			fmt.Fprint(stderr, usageMessage(args))
			return err
		}
		return Snapshot()
	case "packslip":
		cfg, rest, err := parseFlags(args[1:])
		if err != nil {
			fmt.Fprint(stderr, usageMessage(args))
			return err
		}
		if err := requireNoArgs(rest); err != nil {
			fmt.Fprint(stderr, usageMessage(args))
			return err
		}
		if err := requireBin(cfg); err != nil {
			fmt.Fprintln(stderr, err)
			fmt.Fprint(stderr, usageMessage(args))
			return errUsage(args)
		}
		return Packslip(cfg)
	case "publish":
		cfg, rest, err := parseFlags(args[1:])
		if err != nil {
			fmt.Fprint(stderr, usageMessage(args))
			return err
		}
		if err := requireNoArgs(rest); err != nil {
			fmt.Fprint(stderr, usageMessage(args))
			return err
		}
		if err := requireBin(cfg); err != nil {
			fmt.Fprintln(stderr, err)
			fmt.Fprint(stderr, usageMessage(args))
			return errUsage(args)
		}
		return Publish(cfg)
	default:
		fmt.Fprint(stderr, usageMessage(args))
		return errUsage(args)
	}
}

// Usage writes the release help text.
func Usage(w io.Writer) { fmt.Fprint(w, usage) }

// UsageError means the arguments were wrong; the caller prints the usage.
type UsageError struct{ args []string }

func (e *UsageError) Error() string { return "usage" }

func errUsage(args []string) error { return &UsageError{args: args} }

func usageMessage(args []string) string {
	var b strings.Builder
	if len(args) > 0 {
		fmt.Fprintf(&b, "unknown command %q\n\n", strings.Join(args, " "))
	}
	b.WriteString(usage)
	return b.String()
}

// config is what varies per repo: the binary goreleaser builds and the
// packslip resources it ships. Everything else is derived from git.
type config struct {
	bin       string
	resources []string
	version   string // publish only, local mode
}

func parseFlags(args []string) (config, []string, error) {
	var cfg config
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bin":
			v, err := next(args, &i, "--bin")
			if err != nil {
				return cfg, rest, err
			}
			cfg.bin = v
		case "--resource":
			v, err := next(args, &i, "--resource")
			if err != nil {
				return cfg, rest, err
			}
			cfg.resources = append(cfg.resources, v)
		case "--version":
			v, err := next(args, &i, "--version")
			if err != nil {
				return cfg, rest, err
			}
			cfg.version = v
		default:
			rest = append(rest, args[i])
		}
	}
	return cfg, rest, nil
}

func next(args []string, i *int, flag string) (string, error) {
	if *i+1 >= len(args) {
		return "", errUsage([]string{flag + " needs a value"})
	}
	*i++
	return args[*i], nil
}

func requireNoArgs(args []string) error {
	if len(args) > 0 {
		return errUsage(append([]string{"unexpected"}, args...))
	}
	return nil
}

func requireBin(cfg config) error {
	if cfg.bin == "" {
		return fmt.Errorf("--bin is required")
	}
	return nil
}

// run streams a command's output; the caller sees goreleaser/packslip/gh
// exactly as if they had run them by hand.
func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func out(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	raw, err := cmd.Output()
	return strings.TrimSpace(string(raw)), err
}

// slug derives "owner/repo" from the origin remote, so no repo name is
// committed anywhere in the release logic.
func slug() (string, error) {
	url, err := out("", "git", "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("cannot read origin remote: %w", err)
	}
	// Handles git@github.com:owner/repo(.git) and https://github.com/owner/repo(.git).
	rest := url
	if i := strings.Index(rest, "github.com"); i >= 0 {
		rest = rest[i+len("github.com"):]
	}
	rest = strings.TrimPrefix(rest, ":")
	rest = strings.TrimPrefix(rest, "/")
	rest = strings.TrimSuffix(rest, ".git")
	rest = strings.TrimSuffix(rest, "/")
	if strings.Count(rest, "/") != 1 {
		return "", fmt.Errorf("cannot parse owner/repo from origin %q", url)
	}
	return rest, nil
}

func head() (string, error) {
	return out("", "git", "rev-parse", "HEAD")
}

func describe() (string, error) {
	return out("", "git", "describe", "--tags", "--always")
}

func dirty() (bool, error) {
	s, err := out("", "git", "status", "--porcelain")
	return s != "", err
}

// Snapshot builds the release artifacts locally without publishing.
func Snapshot() error {
	return run("", "goreleaser", "release", "--snapshot", "--clean", "--config", ".goreleaser.yml")
}

// keygen mints an ephemeral signing key under dir and returns its path.
// packslip writes the public half next to the key with a .pub extension on
// the stem (foo.key -> foo.pub), so all three spellings are removed first:
// a stale .pub from a previous run otherwise refuses to be overwritten.
func keygen(dir, name string) (string, error) {
	key := filepath.Join(dir, name)
	os.Remove(key)
	os.Remove(key + ".pub")
	os.Remove(strings.TrimSuffix(key, filepath.Ext(key)) + ".pub")
	if err := run("", "packslip", "keygen", "--out", key); err != nil {
		return "", err
	}
	return key, nil
}

// create signs dist/*.tar.gz into dist/packslip.sigstore.json.
func create(slug, version, commit, tag, key string, cfg config, noLog bool) error {
	args := []string{"create",
		"--project", "github.com/" + slug,
		"--version", version,
		"--out", "dist",
		"--bin", cfg.bin,
		"--source-repo", "https://github.com/" + slug,
		"--commit", commit,
		"--tag", tag,
	}
	if key != "" {
		args = append(args, "--key", key)
	}
	if noLog {
		args = append(args, "--no-log")
	}
	for _, r := range cfg.resources {
		args = append(args, "--resource", r)
	}
	matches, err := filepath.Glob("dist/*.tar.gz")
	if err != nil || len(matches) == 0 {
		return fmt.Errorf("no dist/*.tar.gz to sign; run rel snapshot first")
	}
	args = append(args, matches...)
	return run("", "packslip", args...)
}

// Packslip builds the manifest for the local snapshot artifacts and verifies
// it. There is no signing identity locally, so the manifest is unlogged.
func Packslip(cfg config) error {
	requireBin(cfg)
	if err := Snapshot(); err != nil {
		return err
	}
	version, err := describe()
	if err != nil {
		return err
	}
	version = strings.TrimPrefix(strings.TrimPrefix(version, "v"), "v")
	commit, err := head()
	if err != nil {
		return err
	}
	tag, err := describe()
	if err != nil {
		return err
	}
	s, err := slug()
	if err != nil {
		return err
	}
	key, err := keygen(os.TempDir(), "packslip-test.key")
	if err != nil {
		return err
	}
	if err := create(s, version, commit, tag, key, cfg, true); err != nil {
		return err
	}
	// packslip writes the public key as "<id> <base64>"; verify wants the key alone.
	// The public half sits on the stem (foo.key -> foo.pub), not on the full name.
	pub, err := out("", "tail", "-1", strings.TrimSuffix(key, filepath.Ext(key))+".pub")
	if err != nil {
		return err
	}
	b64 := pub
	if i := strings.LastIndex(pub, " "); i >= 0 {
		b64 = pub[i+1:]
	}
	b64file := key + ".b64"
	if err := os.WriteFile(b64file, []byte(b64+"\n"), 0600); err != nil {
		return err
	}
	matches, _ := filepath.Glob("dist/*.tar.gz")
	if err := run("", "packslip", "verify", "dist/packslip.sigstore.json",
		"--pubkey", b64file, "--allow-unlogged", "--artifact", matches[0]); err != nil {
		return err
	}
	return run("", "packslip", "show", "dist/packslip.sigstore.json")
}

// Publish builds the artifacts, signs the manifest, and publishes the GitHub
// Release. In CI (GITHUB_REF_NAME set) goreleaser publishes and packslip signs
// with the workflow identity; locally everything runs here with an ephemeral
// key and gh uploads the result.
func Publish(cfg config) error {
	requireBin(cfg)
	s, err := slug()
	if err != nil {
		return err
	}
	if tag := os.Getenv("GITHUB_REF_NAME"); tag != "" {
		version := strings.TrimPrefix(tag, "v")
		commit := os.Getenv("GITHUB_SHA")
		if err := run("", "goreleaser", "release", "--clean", "--config", ".goreleaser.yml"); err != nil {
			return err
		}
		if err := create(s, version, commit, tag, "", cfg, false); err != nil {
			return err
		}
		return run("", "gh", "release", "upload", tag, "dist/packslip.sigstore.json", "--clobber")
	}
	tag := cfg.version
	if tag == "" {
		return fmt.Errorf("pass --version vX.Y.Z (CI uses GITHUB_REF_NAME instead)")
	}
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	version := strings.TrimPrefix(tag, "v")
	if isDirty, err := dirty(); err != nil {
		return err
	} else if isDirty {
		return fmt.Errorf("working tree is dirty; commit first")
	}
	if err := run("", "git", "tag", tag); err != nil {
		return err
	}
	// Push only the tag: pushing main too can advance the remote past the tag
	// when local main is ahead, and gh then targets the wrong repo state.
	if err := run("", "git", "push", "origin", "refs/tags/"+tag); err != nil {
		return err
	}
	commit, err := head()
	if err != nil {
		return err
	}
	// goreleaser needs a token env even though gh authenticates from its own
	// keychain; reuse it when set, else ask gh for one.
	if os.Getenv("GITHUB_TOKEN") == "" {
		if token, err := out("", "gh", "auth", "token"); err == nil && token != "" {
			os.Setenv("GITHUB_TOKEN", token)
		}
	}
	notes := "Release " + tag
	if err := run("", "goreleaser", "release", "--clean", "--config", ".goreleaser.yml",
		"--release-notes", notes); err != nil {
		// goreleaser takes --release-notes as a file; fall back to a temp file.
		f, ferr := os.CreateTemp("", "rel-notes-*.md")
		if ferr != nil {
			return err
		}
		defer os.Remove(f.Name())
		fmt.Fprintln(f, notes)
		f.Close()
		if err := run("", "goreleaser", "release", "--clean", "--config", ".goreleaser.yml",
			"--release-notes", f.Name()); err != nil {
			return err
		}
	}
	key, err := keygen(os.TempDir(), "packslip-local.key")
	if err != nil {
		return err
	}
	if err := create(s, version, commit, tag, key, cfg, true); err != nil {
		return err
	}
	// goreleaser already created the release when it published; upload into it,
	// or create it when goreleaser ran without a token to do so itself.
	files := []string{"dist/packslip.sigstore.json"}
	matches, _ := filepath.Glob("dist/*.tar.gz")
	files = append(files, matches...)
	files = append(files, "dist/checksums.txt")
	args := append([]string{"release", "upload", tag}, files...)
	args = append(args, "--clobber")
	if err := run("", "gh", args...); err != nil {
		args := append([]string{"release", "create", tag}, files...)
		args = append(args, "--title", tag, "--notes", notes)
		if err := run("", "gh", args...); err != nil {
			return err
		}
	}
	fmt.Println("published", tag)
	return nil
}
