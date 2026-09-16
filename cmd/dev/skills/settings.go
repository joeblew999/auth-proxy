package skills

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

// ownedKeys are the settings generated from skills.toml. A key absent from
// wantSettings is removed from the file, so turning a pin off in skills.toml
// takes the setting with it.
var ownedKeys = []string{"enabledPlugins", "disableClaudeAiConnectors", "enableAllProjectMcpServers"}

// wantSettings is what [claude] in skills.toml means in settings.json terms.
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
	if !c.ClaudeAIConnectors {
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

// checkSettings fails when settings.json has drifted from skills.toml, which
// is what happens when someone edits the settings file by hand.
func checkSettings(c claudePins) error {
	have, err := readSettings()
	if err != nil {
		return err
	}
	if diff := diffSettings(have, wantSettings(c)); len(diff) > 0 {
		return fmt.Errorf("%s does not match [claude] in %s:\n%sfix with: mise run dev:skills:sync",
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
		return nil, fmt.Errorf("%s: %w; fix the JSON, then: mise run dev:skills:sync", settingsFile, err)
	}
	return settings, nil
}
