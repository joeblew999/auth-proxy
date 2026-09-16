package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
)

// The repo's own Claude Code settings. Only the keys below are written here;
// everything else in the file (the Stop hook, anything a developer adds) is
// left exactly as it was.
const settingsFile = ".claude/settings.json"

// ownedKeys are the settings generated from session.toml. A key absent from
// wantSettings is removed from the file, so turning a pin off in session.toml
// takes the setting with it.
var ownedKeys = []string{"enabledPlugins", "disableClaudeAiConnectors", "enableAllProjectMcpServers"}

// wantSettings is what [claude] in session.toml means in settings.json terms.
//
// enabledPlugins is read user < project < local, so a false here overrides a
// developer's own true: the repo decides, not whoever installed a marketplace
// plugin once. disableClaudeAiConnectors is any-source-true, so the repo can
// opt out but cannot force connectors back on.
func wantSettings(c claudePins) map[string]any {
	want := map[string]any{}
	if len(c.BlockedPlugins) > 0 {
		blocked := map[string]any{}
		for _, name := range c.BlockedPlugins {
			blocked[name] = false
		}
		want["enabledPlugins"] = blocked
	}
	// Only a declared "off" writes anything: the setting is any-source-true,
	// so a repo can opt out of connectors but cannot turn them back on.
	if c.ClaudeAIConnectors != nil && !*c.ClaudeAIConnectors {
		want["disableClaudeAiConnectors"] = true
	}
	if c.ApproveMCPServers {
		want["enableAllProjectMcpServers"] = true
	}
	return want
}

// syncSettings merges the generated keys into settings.json, preserving the
// rest of the file.
func syncSettings(out io.Writer, c claudePins) error {
	have, err := readSettings()
	if err != nil {
		return err
	}
	want := wantSettings(c)
	if diff := diffSettings(have, want); len(diff) == 0 {
		return nil
	}
	for _, key := range ownedKeys {
		if value, ok := want[key]; ok {
			have[key] = value
		} else {
			delete(have, key)
		}
	}
	data, err := json.MarshalIndent(have, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsFile, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("%s: %w", settingsFile, err)
	}
	fmt.Fprintf(out, "\n%s now blocks %d marketplace plugin(s) from shadowing these skills.\n",
		settingsFile, len(c.BlockedPlugins))
	return nil
}

// checkSettings fails when settings.json has drifted from session.toml, which
// is what happens when someone edits the settings file by hand.
func checkSettings(c claudePins) error {
	have, err := readSettings()
	if err != nil {
		return err
	}
	if diff := diffSettings(have, wantSettings(c)); len(diff) > 0 {
		return fmt.Errorf("%s does not match [claude] in %s:\n%sfix with: "+syncCmd,
			settingsFile, pinsFile, indent(strings.Join(diff, "\n")))
	}
	return nil
}

// diffSettings compares only the generated keys; the rest of the file is none
// of this package's business.
func diffSettings(have, want map[string]any) []string {
	var diff []string
	for _, key := range ownedKeys {
		haveValue, haveOK := have[key]
		wantValue, wantOK := want[key]
		switch {
		case wantOK && !haveOK:
			diff = append(diff, "missing: "+key)
		case !wantOK && haveOK:
			diff = append(diff, "unexpected: "+key)
		case wantOK && !reflect.DeepEqual(haveValue, wantValue):
			diff = append(diff, "changed: "+key)
		}
	}
	sort.Strings(diff)
	return diff
}

// readSettings returns the settings file, or an empty set when there is none
// yet. A malformed file is an error: overwriting it would throw away hooks.
func readSettings() (map[string]any, error) {
	data, err := os.ReadFile(settingsFile)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("%s: %w; fix the JSON, then: "+syncCmd, settingsFile, err)
	}
	return settings, nil
}

// committedClaudeFiles are the Claude Code files a repo checks in. Claude Code
// rewrites a command it launches to the absolute path it resolved, so a file
// that was portable when written comes back naming one machine's toolchain.
// Committing that breaks every other clone, quietly, on a machine nobody is
// looking at.
var committedClaudeFiles = []string{settingsFile, ".mcp.json"}

// checkPortablePaths fails when one of those files names a command by absolute
// path. A bare name is found on PATH wherever the repo is cloned.
func checkPortablePaths() error {
	var bad []string
	for _, name := range committedClaudeFiles {
		data, err := os.ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			// Malformed JSON is reported by whoever owns the file.
			continue
		}
		for _, command := range commandValues(parsed) {
			if strings.HasPrefix(command, "/") {
				bad = append(bad, fmt.Sprintf("%s: %s", name, command))
			}
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("a command is named by absolute path, so it only works on the machine that wrote it:\n%sname it bare (\"mise\", not \"/opt/homebrew/bin/mise\") so PATH finds it in every clone",
			indent(strings.Join(bad, "\n")))
	}
	return nil
}

// commandValues collects every "command" string anywhere in the document,
// whatever shape the file uses to nest them.
func commandValues(node any) []string {
	var found []string
	switch v := node.(type) {
	case map[string]any:
		for key, value := range v {
			if key == "command" {
				if command, ok := value.(string); ok {
					found = append(found, command)
				}
			}
			found = append(found, commandValues(value)...)
		}
	case []any:
		for _, item := range v {
			found = append(found, commandValues(item)...)
		}
	}
	return found
}
