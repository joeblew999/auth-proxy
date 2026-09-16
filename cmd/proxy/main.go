//go:build !js || !wasm

// Command grok-oauth-proxy is an OpenAI-compatible proxy for many providers.
// The same handler also runs as a Cloudflare Worker (cmd/worker).
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/bootstrap"
	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/config"
	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/mcp"
	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/proxy"
	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/xaiauth"
)

const (
	localAddr = "127.0.0.1:56121"
	localURL  = "http://" + localAddr
	// The Grok OAuth client only accepts this exact redirect URI.
	redirectURI = localURL + "/callback"
)

const usage = `grok-oauth-proxy: one OpenAI-compatible endpoint for many model providers.

Commands (usually run through mise; see "mise tasks"):
  serve  [--config FILE] [--addr ADDR]      run the proxy locally (default command)
  status [--config FILE] [--url URL]        check providers and print how to fix problems
  models [--url URL]                        list model IDs across all providers
  chat   [--url URL] MODEL [PROMPT]         send a prompt and stream the answer
  login  [--config FILE] [--url URL]        log in to Grok (browser locally, device code for --url)
  secrets [--config FILE] [PROVIDER|admin]  print the secret names providers.toml needs, as NAME<TAB>OWNER
  validate [--config FILE]                  fail on any mistake in providers.toml, say nothing otherwise

--url talks to a running proxy (such as the deployed Worker) using ADMIN_API_KEY.
Providers come from providers.toml, built into the binary unless --config is given.
`

func main() {
	args := os.Args[1:]
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	commands := map[string]func([]string) error{
		"serve": serve, "status": status, "models": models, "chat": chat, "login": login, "secrets": secrets, "validate": validate,
	}
	run, ok := commands[command]
	switch {
	case command == "help":
		fmt.Print(usage)
		return
	case !ok:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(2)
	}
	if err := run(args); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

// errReported means the command already printed why it failed.
var errReported = errors.New("reported")

func flags(name string, args []string, define func(*flag.FlagSet)) (*flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	define(fs)
	return fs, fs.Parse(args)
}

func serve(args []string) error {
	var configPath, addr string
	if _, err := flags("serve", args, func(fs *flag.FlagSet) {
		fs.StringVar(&configPath, "config", "", "providers file")
		fs.StringVar(&addr, "addr", localAddr, "listen address")
	}); err != nil {
		return err
	}
	cfg, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		return err
	}
	up, err := localUpstream(cfg, &http.Client{})
	if err != nil {
		return err
	}
	opts := proxy.Options{Upstream: up, MCP: mcp.NewHandler(up)}
	if up.XAI != nil {
		browser := up.XAI.NewBrowserLogin(redirectURI)
		opts.Public = map[string]http.HandlerFunc{"/login": browser.Start, "/callback": browser.Callback}
	}

	printStatus(os.Stdout, cfg.Source, cfg.Default, cfg.AdminKey != "", up.Status())
	log.Printf("listening on http://%s (OpenAI base URL: http://%s/v1)", addr, addr)
	return http.ListenAndServe(addr, proxy.NewHandler(opts))
}

func localUpstream(cfg *config.Config, client *http.Client) (*proxy.Upstream, error) {
	up := &proxy.Upstream{Config: cfg, Client: client, LoginFix: "mise run login"}
	if cfg.OAuthProvider() != nil {
		store, err := xaiauth.DefaultFileStore()
		if err != nil {
			return nil, err
		}
		up.XAI = xaiauth.New(store, client)
	}
	return up, nil
}

func status(args []string) error {
	var configPath, url string
	if _, err := flags("status", args, func(fs *flag.FlagSet) {
		fs.StringVar(&configPath, "config", "", "providers file")
		fs.StringVar(&url, "url", "", "check a running proxy instead of the local configuration")
	}); err != nil {
		return err
	}

	var (
		source, def string
		adminSet    bool
		providers   []proxy.ProviderStatus
	)
	if url == "" {
		cfg, err := bootstrap.LoadConfig(configPath)
		if err != nil {
			return err
		}
		up, err := localUpstream(cfg, http.DefaultClient)
		if err != nil {
			return err
		}
		source, def, adminSet, providers = cfg.Source, cfg.Default, cfg.AdminKey != "", up.Status()
	} else {
		var report struct {
			Source    string                 `json:"source"`
			Default   string                 `json:"default"`
			Providers []proxy.ProviderStatus `json:"providers"`
		}
		if err := callProxy(http.MethodGet, url, "/admin/status", nil, &report); err != nil {
			return err
		}
		source, def, adminSet, providers = url+" ("+report.Source+")", report.Default, true, report.Providers
	}

	if !printStatus(os.Stdout, source, def, adminSet, providers) {
		return errReported
	}
	return nil
}

// printStatus writes a readiness table and reports whether everything is ready.
func printStatus(out io.Writer, source, def string, adminSet bool, providers []proxy.ProviderStatus) bool {
	ready := adminSet
	fmt.Fprintf(out, "config:  %s\ndefault: %s\n", source, def)
	if adminSet {
		fmt.Fprintln(out, "client key: set")
	} else {
		fmt.Fprintf(out, "client key: %s is not set -> mise run secrets:set admin\n", config.AdminKeyName)
	}
	fmt.Fprintln(out)
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	for _, p := range providers {
		mark, note := "ok", ""
		if !p.Ready {
			mark, note, ready = "!!", p.Problem+" -> "+p.Fix, false
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", mark, p.Name, p.Auth, p.BaseURL, note)
	}
	tw.Flush()
	return ready
}

func models(args []string) error {
	var url string
	if _, err := flags("models", args, func(fs *flag.FlagSet) {
		fs.StringVar(&url, "url", localURL, "proxy URL")
	}); err != nil {
		return err
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := callProxy(http.MethodGet, url, "/v1/models", nil, &list); err != nil {
		return err
	}
	for _, m := range list.Data {
		fmt.Println(m.ID)
	}
	return nil
}

func chat(args []string) error {
	var url string
	fs, err := flags("chat", args, func(fs *flag.FlagSet) {
		fs.StringVar(&url, "url", localURL, "proxy URL")
	})
	if err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: chat [--url URL] MODEL [PROMPT]; list models with: mise run models")
	}
	model, prompt := fs.Arg(0), strings.Join(fs.Args()[1:], " ")
	if prompt == "" {
		prompt = "Say hello in one sentence."
	}
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"stream":   true,
		"messages": []any{map[string]string{"role": "user", "content": prompt}},
	})
	resp, err := proxyRequest(http.MethodPost, url, "/v1/chat/completions", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) == nil && len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	fmt.Println()
	return scanner.Err()
}

func login(args []string) error {
	var configPath, url string
	if _, err := flags("login", args, func(fs *flag.FlagSet) {
		fs.StringVar(&configPath, "config", "", "providers file")
		fs.StringVar(&url, "url", "", "log in a running proxy (such as the Worker) with the device code flow")
	}); err != nil {
		return err
	}
	if url != "" {
		return deviceLogin(url)
	}

	cfg, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		return err
	}
	up, err := localUpstream(cfg, http.DefaultClient)
	if err != nil {
		return err
	}
	if up.XAI == nil {
		return fmt.Errorf("no provider in %s uses auth = \"xai-oauth\", so there is nothing to log in to; for a SuperGrok subscription, set auth = \"xai-oauth\" (instead of key) on the api.x.ai provider", cfg.Source)
	}
	browser := up.XAI.NewBrowserLogin(redirectURI)
	mux := http.NewServeMux()
	mux.HandleFunc("/login", browser.Start)
	mux.HandleFunc("/callback", browser.Callback)
	server := &http.Server{Addr: localAddr, Handler: mux}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	fmt.Printf("Opening %s/login in your browser...\n", localURL)
	openBrowser(localURL + "/login")
	select {
	case <-browser.Done():
		store, _ := xaiauth.DefaultFileStore()
		fmt.Println("Logged in to Grok. Tokens saved to", store.TokensPath())
		time.Sleep(500 * time.Millisecond) // let the browser receive the success page
		return nil
	case err := <-serveErr:
		return fmt.Errorf("cannot listen on %s (%v); if the proxy is running, open %s/login instead", localAddr, err, localURL)
	}
}

func deviceLogin(url string) error {
	var state xaiauth.DeviceStatus
	if err := callProxy(http.MethodPost, url, "/admin/auth/start", nil, &state); err != nil {
		return err
	}
	if state.Status == "pending" {
		fmt.Printf("Open %s and enter code %s\n", state.VerificationURL, state.UserCode)
		openBrowser(state.VerificationURL)
	}
	for !state.Terminal() {
		time.Sleep(time.Duration(max(state.RetryAfterSeconds, 1)) * time.Second)
		if err := callProxy(http.MethodPost, url, "/admin/auth/status", nil, &state); err != nil {
			return err
		}
	}
	if state.Status != "authenticated" {
		return fmt.Errorf("Grok login %s; run it again: mise run login --worker", state.Status)
	}
	fmt.Println("Logged in to Grok on", url)
	return nil
}

// validate loads the providers file and says nothing when it is sound; deploy
// runs it first, so a mistake never reaches the Worker.
func validate(args []string) error {
	var configPath string
	if _, err := flags("validate", args, func(fs *flag.FlagSet) {
		fs.StringVar(&configPath, "config", "", "providers file")
	}); err != nil {
		return err
	}
	_, err := bootstrap.LoadConfig(configPath)
	return err
}

func secrets(args []string) error {
	var configPath string
	fs, err := flags("secrets", args, func(fs *flag.FlagSet) {
		fs.StringVar(&configPath, "config", "", "providers file")
	})
	if err != nil {
		return err
	}
	cfg, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fmt.Printf("%s\tadmin\n", config.AdminKeyName)
		for _, p := range cfg.Providers {
			if p.Auth == config.AuthKey {
				fmt.Printf("%s\t%s\n", p.KeyName, p.Name)
			}
		}
		return nil
	}
	name := fs.Arg(0)
	if name == "admin" {
		fmt.Println(config.AdminKeyName)
		return nil
	}
	p := cfg.Provider(name)
	switch {
	case p == nil:
		return fmt.Errorf("no provider %q in %s (providers: %s, or admin for the client key)", name, cfg.Source, strings.Join(cfg.Names(), ", "))
	case p.Auth == config.AuthXAIOAuth:
		return fmt.Errorf("provider %s uses the Grok login, not a key; run: mise run login", name)
	case p.Auth == config.AuthNone:
		return fmt.Errorf("provider %s uses auth = \"none\" and needs no key", name)
	}
	fmt.Println(p.KeyName)
	return nil
}

// proxyRequest calls a running proxy with ADMIN_API_KEY and turns proxy errors
// into their message and fix.
func proxyRequest(method, baseURL, path string, body []byte) (*http.Response, error) {
	key := os.Getenv(config.AdminKeyName)
	if key == "" {
		return nil, fmt.Errorf("%s is not set; run this through mise so fnox provides it, or set it with: mise run secrets:set admin", config.AdminKeyName)
	}
	req, err := http.NewRequest(method, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %v (is it running? locally: mise run dev)", baseURL, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		var perr struct {
			Error struct {
				Message string `json:"message"`
				Fix     string `json:"fix"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &perr) == nil && perr.Error.Message != "" {
			if perr.Error.Fix != "" && !strings.Contains(perr.Error.Message, perr.Error.Fix) {
				return nil, fmt.Errorf("%s (HTTP %d) -> %s", perr.Error.Message, resp.StatusCode, perr.Error.Fix)
			}
			return nil, fmt.Errorf("%s (HTTP %d)", perr.Error.Message, resp.StatusCode)
		}
		return nil, fmt.Errorf("%s%s returned HTTP %d: %s", baseURL, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return resp, nil
}

func callProxy(method, baseURL, path string, body []byte, out any) error {
	resp, err := proxyRequest(method, baseURL, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
