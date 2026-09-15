package views

import (
	"github.com/joeblew999/grok-oauth-proxy/spikes/hello-world/ui"
)

// Mode is where models run: remote, local or in the browser.
type Mode struct {
	ID        string
	Label     string
	Blurb     string
	Available bool
	Action    string // shown when not available
}

// Model is one model within a mode.
type Model struct {
	ID       string
	Ready    bool
	Status   string
	Progress float64 // download percent for in-browser models; 0 when not downloading
}

// ModePicker shows the three modes; the selected one is resolved by the caller.
component ModePicker(modes []Mode, current string) {
	<ui.Tabs value={current}>
		<ui.TabsList>
			{ for _, m := range modes {
				<ui.TabsTrigger value={m.ID} selected={m.ID == current}>{ m.Label }</ui.TabsTrigger>
			} }
		</ui.TabsList>
		{ for _, m := range modes {
			<ui.TabsContent value={m.ID} selected={m.ID == current}>
				<p>{ m.Blurb }</p>
				{ if !m.Available {
					<ui.Badge variant="destructive">{ m.Action }</ui.Badge>
				} }
			</ui.TabsContent>
		} }
	</ui.Tabs>
}

// ModelList shows only the current mode's models, each with its status.
component ModelList(models []Model) {
	<ui.ItemGroup>
		{ for _, m := range models {
			<ui.Item variant="outline">
				<ui.ItemContent>
					<ui.ItemTitle>{ m.ID }</ui.ItemTitle>
					<ui.ItemDescription>{ m.Status }</ui.ItemDescription>
					{ if m.Progress > 0 {
						<ui.Progress value={m.Progress}/>
					} }
				</ui.ItemContent>
				<ui.ItemActions>
					{ if m.Ready {
						<ui.Badge>ready</ui.Badge>
					} else {
						<ui.Button size="sm" variant="outline">{ m.Status }</ui.Button>
					} }
				</ui.ItemActions>
			</ui.Item>
		} }
	</ui.ItemGroup>
}

// DemoModes and DemoModels are sample data for validating the toolchain.
var DemoModes = []Mode{
	{ID: "remote", Label: "Remote", Blurb: "Runs at the provider and costs credits.", Available: true},
	{ID: "local", Label: "Local", Blurb: "Runs on your machine.", Action: "Start kronk"},
	{ID: "browser", Label: "In browser", Blurb: "Private; runs on this device.", Available: true},
}

var DemoModels = []Model{
	{ID: "browser/Qwen3-0.6B", Ready: true, Status: "downloaded"},
	{ID: "browser/Llama-3.2-1B", Status: "Download 770 MB", Progress: 42},
}

// Picker is the mode picker followed by that mode's models.
component Picker(modes []Mode, current string, models []Model) {
	<ModePicker modes={modes} current={current}/>
	<ModelList models={models}/>
}
