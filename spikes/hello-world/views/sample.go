package views

import "github.com/joeblew999/grok-oauth-proxy/spikes/hello-world/ui/icon"

// SampleModes stands in for what the proxy will report once the three runtimes
// are wired up. The shapes are the real ones the GUI will be handed; the values
// are made up, and every state the picker can draw appears at least once.
var SampleModes = []Mode{
	{
		ID:    "remote",
		Label: "Remote",
		Blurb: "Runs at the provider. Fastest and largest, costs credits, and the prompt leaves this machine.",
		Icon:  icon.Cloud,
		Ready: true,
		Models: []Model{
			{ID: "xai/grok-4.3", Detail: "SuperGrok subscription", State: Ready},
			{ID: "xai/grok-4.3-fast", Detail: "SuperGrok subscription", State: Ready},
			{ID: "groq/llama-3.3-70b", Detail: "API key", State: Ready},
		},
	},
	{
		ID:    "local",
		Label: "Local",
		Blurb: "Runs on this machine through kronk. Private and free, and it uses your own CPU and GPU.",
		Icon:  icon.Cpu,
		Fix: Fix{
			Problem: "Nothing is answering on 127.0.0.1:11435",
			Detail:  "Local models are served by kronk. Check every provider and print the fix for each one:",
			Command: "mise run status",
		},
	},
	{
		ID:    "browser",
		Label: "In browser",
		Blurb: "Runs in this browser on WebGPU. Private, works offline, and each model downloads once.",
		Icon:  icon.Globe,
		Ready: true,
		Models: []Model{
			{ID: "browser/Qwen3-0.6B", Detail: "520 MB, already downloaded", State: Ready},
			{ID: "browser/Llama-3.2-1B", Detail: "770 MB, downloading", State: Downloading, Progress: 42},
			{ID: "browser/Qwen3-4B", Detail: "2.3 GB, not downloaded", State: Absent, Action: "Download"},
		},
	},
}
