package worker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// deployCopy is what wrangler actually deploys from. wrangler writes the ids
// of resources it provisions back into that file, so deploying from a copy
// keeps wrangler.toml identical for every developer and git clean. It sits in
// the same directory, so every relative path in it still holds.
const deployCopy = "wrangler.deploy.toml"

// idKeys are the binding kinds wrangler provisions, and the key it writes back.
var idKeys = map[string]string{
	"kv_namespaces": "id",
	"d1_databases":  "database_id",
	"r2_buckets":    "bucket_name",
}

// Deploy runs `wrangler deploy` for env in the Worker's dir, from a throwaway
// copy of its wrangler.toml, and then says what wrangler created on this
// account, or that it inherited the deployed Worker's bindings, which is what
// happens on every deploy after the first.
func Deploy(out io.Writer, dir, env string) error {
	src := filepath.Join(dir, wranglerFile)
	copyPath := filepath.Join(dir, deployCopy)
	before, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	if err := os.WriteFile(copyPath, before, 0o644); err != nil {
		return err
	}
	defer os.Remove(copyPath)
	if err := fnoxExec(dir, nil, out, "wrangler", "deploy", "--config", deployCopy, "--env", env); err != nil {
		return fmt.Errorf("wrangler deploy failed: %w", err)
	}
	after, err := os.ReadFile(copyPath)
	if err != nil {
		return err
	}
	made, err := created(before, after)
	if err != nil {
		return fmt.Errorf("reading what wrangler wrote back to %s: %w", copyPath, err)
	}
	if len(made) == 0 {
		fmt.Fprintln(out, "bindings inherited from the deployed Worker; nothing written back")
		return nil
	}
	for _, m := range made {
		fmt.Fprintln(out, "created and linked:", m)
	}
	return nil
}

// bindingIDs maps "<env>/<kind>/<binding>" to the id the config gives it, ""
// when it gives none. The top-level environment is "".
func bindingIDs(doc map[string]any) map[string]string {
	ids := map[string]string{}
	collect := func(where string, table map[string]any) {
		for kind, idKey := range idKeys {
			for _, e := range entriesOf(table[kind]) {
				name, _ := e["binding"].(string)
				id, _ := e[idKey].(string)
				ids[where+"/"+kind+"/"+name] = id
			}
		}
	}
	collect("", doc)
	if envs, ok := doc["env"].(map[string]any); ok {
		for name, t := range envs {
			if table, ok := t.(map[string]any); ok {
				collect(name, table)
			}
		}
	}
	return ids
}

// created lists the bindings that have an id in after but not in before: the
// resources wrangler provisioned on this deploy.
func created(before, after []byte) ([]string, error) {
	var a, b map[string]any
	if err := toml.Unmarshal(before, &a); err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(after, &b); err != nil {
		return nil, err
	}
	was, now := bindingIDs(a), bindingIDs(b)
	var out []string
	for key, id := range now {
		if id == "" || was[key] != "" {
			continue
		}
		parts := strings.SplitN(key, "/", 3)
		line := fmt.Sprintf("%s %s %s=%s", parts[1], parts[2], idKeys[parts[1]], id)
		if parts[0] != "" {
			line += " (env " + parts[0] + ")"
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}

// entriesOf is an array of tables however the decoder hands it back: inline
// tables come as []any, [[table]] blocks as []map[string]any.
func entriesOf(v any) []map[string]any {
	switch arr := v.(type) {
	case []map[string]any:
		return arr
	case []any:
		var out []map[string]any
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
