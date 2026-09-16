// Package release publishes a GitHub Release of one command directory:
// goreleaser builds the artifacts, packslip signs the manifest, gh uploads.
// The same command runs locally and in CI; in CI (GITHUB_REF_NAME set) the
// tag is the one pushed and packslip signs with the workflow's identity.
//
// Nothing is configured per repo. The binary is named after the repo, every
// directory under skills/ ships as a skill, and the goreleaser config is
// generated from the command directory unless the repo keeps its own.
package release

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
)

const usage = `dev release DIR [VERSION] [--snapshot] [--name NAME]
    publish a GitHub Release of the command in DIR: tag VERSION (vX.Y.Z; in CI
    the pushed tag), build every platform with goreleaser, sign the packslip
    manifest, upload. --snapshot builds, signs with a throwaway key and
    verifies, publishing nothing. NAME is the binary's name; default the
    repo's. Every directory under skills/ ships as a skill.

Needs goreleaser, packslip and gh, and a clean tree to publish.
`

// UsageError means the arguments were wrong; the caller prints the usage.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

// Usage is the help text cmd/dev prints for this command.
func Usage() string { return usage }

// Run dispatches. Args are everything after "release".
func Run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var snapshot cli.Bool
	fs.Var(&snapshot, "snapshot", "build, sign with a throwaway key and verify; publish nothing")
	name := fs.String("name", "", "the binary's name (default: the repo's)")
	if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return &UsageError{"release: the command directory comes first"}
	}
	dir := args[0]
	rest, err := cli.ParseInterleaved(fs, args[1:])
	if err != nil {
		return &UsageError{"release: " + err.Error()}
	}
	if len(rest) > 1 {
		return &UsageError{"release: at most one VERSION"}
	}
	version := ""
	if len(rest) == 1 {
		version = rest[0]
	}
	r, err := newRelease(dir, *name)
	if err != nil {
		return err
	}
	if snapshot {
		return r.snapshot()
	}
	return r.publish(version)
}

// release is one command directory as goreleaser and packslip see it.
type release struct {
	dir, name, slug string
	skills          []string // packslip resources, one per skills/<name>
	config          string   // the goreleaser config to use
	cleanup         func()
}

func newRelease(dir, name string) (*release, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	s, err := slug()
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = s[strings.LastIndex(s, "/")+1:]
	}
	r := &release{dir: dir, name: name, slug: s, cleanup: func() {}}
	entries, _ := os.ReadDir("skills")
	for _, e := range entries {
		if e.IsDir() {
			r.skills = append(r.skills, fmt.Sprintf("skill/%s=repo:skills/%s", e.Name(), e.Name()))
		}
	}
	r.config = ".goreleaser.yml"
	if _, err := os.Stat(r.config); err != nil {
		f, err := os.CreateTemp("", "goreleaser-*.yml")
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString(goreleaserConfig(name, dir)); err != nil {
			return nil, err
		}
		f.Close()
		r.config = f.Name()
		r.cleanup = func() { os.Remove(f.Name()) }
	}
	return r, nil
}

// goreleaserConfig is the one every repo on this stack would otherwise copy:
// a static binary for the platforms developers and CI run, tar.gz archives,
// checksums, and the release created in the repo goreleaser runs in.
func goreleaserConfig(name, dir string) string {
	return fmt.Sprintf(`version: 2
project_name: %s
builds:
  - id: %s
    dir: %s
    main: .
    binary: %s
    env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    flags: [-trimpath, -buildvcs=false]
    ldflags: [-s -w -buildid=]
archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    wrap_in_directory: false
    files: []
checksum:
  name_template: checksums.txt
release:
  replace_existing_artifacts: true
changelog:
  sort: asc
  filters:
    exclude: ["^docs:", "^test:", "^ci:", "^chore:", "Merge pull request", "Merge branch"]
`, name, name, dir, name)
}

// run streams a command's output; the caller sees goreleaser, packslip and gh
// exactly as if they had run them by hand.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func out(name string, args ...string) (string, error) {
	raw, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(raw)), err
}

// slug derives "owner/repo" from the origin remote, so no repo name is
// written anywhere.
func slug() (string, error) {
	url, err := out("git", "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("cannot read origin remote: %w", err)
	}
	rest := url
	if i := strings.Index(rest, "github.com"); i >= 0 {
		rest = rest[i+len("github.com"):]
	}
	rest = strings.TrimPrefix(strings.TrimPrefix(rest, ":"), "/")
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, ".git"), "/")
	if strings.Count(rest, "/") != 1 {
		return "", fmt.Errorf("cannot parse owner/repo from origin %q", url)
	}
	return rest, nil
}

// keygen mints an ephemeral signing key under dir and returns its path. packslip
// writes the public half on the stem (foo.key -> foo.pub); a stale one refuses
// to be overwritten, so every spelling is removed first.
func keygen(name string) (string, error) {
	key := filepath.Join(os.TempDir(), name)
	os.Remove(key)
	os.Remove(key + ".pub")
	os.Remove(strings.TrimSuffix(key, filepath.Ext(key)) + ".pub")
	if err := run("packslip", "keygen", "--out", key); err != nil {
		return "", err
	}
	return key, nil
}

// create signs dist/*.tar.gz into dist/packslip.sigstore.json.
func (r *release) create(version, commit, tag, key string, noLog bool) error {
	args := []string{"create",
		"--project", "github.com/" + r.slug,
		"--version", version,
		"--out", "dist",
		"--bin", r.name,
		"--source-repo", "https://github.com/" + r.slug,
		"--commit", commit,
		"--tag", tag,
	}
	if key != "" {
		args = append(args, "--key", key)
	}
	if noLog {
		args = append(args, "--no-log")
	}
	for _, s := range r.skills {
		args = append(args, "--resource", s)
	}
	matches, err := filepath.Glob("dist/*.tar.gz")
	if err != nil || len(matches) == 0 {
		return fmt.Errorf("no dist/*.tar.gz to sign; goreleaser built nothing")
	}
	return run("packslip", append(args, matches...)...)
}

// snapshot builds the artifacts, signs the manifest with a throwaway key and
// verifies it, then shows it. Nothing is tagged or uploaded.
func (r *release) snapshot() error {
	defer r.cleanup()
	if err := run("goreleaser", "release", "--snapshot", "--clean", "--config", r.config); err != nil {
		return err
	}
	tag, err := out("git", "describe", "--tags", "--always")
	if err != nil {
		return err
	}
	commit, err := out("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	key, err := keygen("packslip-snapshot.key")
	if err != nil {
		return err
	}
	if err := r.create(strings.TrimPrefix(tag, "v"), commit, tag, key, true); err != nil {
		return err
	}
	// packslip writes the public key as "<id> <base64>"; verify wants the base64 alone.
	pub, err := out("tail", "-1", strings.TrimSuffix(key, filepath.Ext(key))+".pub")
	if err != nil {
		return err
	}
	if i := strings.LastIndex(pub, " "); i >= 0 {
		pub = pub[i+1:]
	}
	pubFile := key + ".b64"
	if err := os.WriteFile(pubFile, []byte(pub+"\n"), 0o600); err != nil {
		return err
	}
	matches, _ := filepath.Glob("dist/*.tar.gz")
	if err := run("packslip", "verify", "dist/packslip.sigstore.json", "--pubkey", pubFile, "--allow-unlogged", "--artifact", matches[0]); err != nil {
		return err
	}
	return run("packslip", "show", "dist/packslip.sigstore.json")
}

// publish tags, builds, signs and uploads. In CI the tag is GITHUB_REF_NAME
// and goreleaser publishes with the workflow's token; locally the tag is
// version, the key is ephemeral, and gh uploads.
func (r *release) publish(version string) error {
	defer r.cleanup()
	if tag := os.Getenv("GITHUB_REF_NAME"); tag != "" {
		if err := run("goreleaser", "release", "--clean", "--config", r.config); err != nil {
			return err
		}
		if err := r.create(strings.TrimPrefix(tag, "v"), os.Getenv("GITHUB_SHA"), tag, "", false); err != nil {
			return err
		}
		return run("gh", "release", "upload", tag, "dist/packslip.sigstore.json", "--clobber")
	}
	if version == "" {
		return fmt.Errorf("give the version to release, vX.Y.Z (CI takes it from the pushed tag)")
	}
	tag := version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	if status, err := out("git", "status", "--porcelain"); err != nil {
		return err
	} else if status != "" {
		return fmt.Errorf("working tree is dirty; commit first")
	}
	if err := run("git", "tag", tag); err != nil {
		return err
	}
	// Push only the tag: pushing main too can advance the remote past the tag
	// when local main is ahead, and gh then targets the wrong repo state.
	if err := run("git", "push", "origin", "refs/tags/"+tag); err != nil {
		return err
	}
	commit, err := out("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	// goreleaser needs a token env even though gh authenticates from its own
	// keychain; reuse it when set, else ask gh for one.
	if os.Getenv("GITHUB_TOKEN") == "" {
		if token, err := out("gh", "auth", "token"); err == nil && token != "" {
			os.Setenv("GITHUB_TOKEN", token)
		}
	}
	notes, err := os.CreateTemp("", "release-notes-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(notes.Name())
	fmt.Fprintln(notes, "Release "+tag)
	notes.Close()
	if err := run("goreleaser", "release", "--clean", "--config", r.config, "--release-notes", notes.Name()); err != nil {
		return err
	}
	key, err := keygen("packslip-local.key")
	if err != nil {
		return err
	}
	if err := r.create(strings.TrimPrefix(tag, "v"), commit, tag, key, true); err != nil {
		return err
	}
	// goreleaser created the release when it published; upload into it, or
	// create it when goreleaser ran without a token to do so itself.
	files := []string{"dist/packslip.sigstore.json", "dist/checksums.txt"}
	matches, _ := filepath.Glob("dist/*.tar.gz")
	files = append(files, matches...)
	if err := run("gh", append(append([]string{"release", "upload", tag}, files...), "--clobber")...); err != nil {
		if err := run("gh", append(append([]string{"release", "create", tag}, files...), "--title", tag, "--notes", "Release "+tag)...); err != nil {
			return err
		}
	}
	fmt.Printf("published %s %s: https://github.com/%s/releases/tag/%s\n", r.name, tag, r.slug, tag)
	return nil
}
