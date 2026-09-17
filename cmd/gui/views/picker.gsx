// Package views holds the spike's pages. The picker is the one that matters:
// it is the project's routing rule made visible.
package views

import (
	"github.com/gsxhq/gsx"

	"github.com/joeblew999/auth-proxy/cmd/gui/ui"
	"github.com/joeblew999/auth-proxy/cmd/gui/ui/icon"
)

// One routing rule decides everything: a model ID carries a provider prefix,
// the prefix names the provider, and the provider says where the model runs.
// So a Mode owns its models rather than sitting beside them — picking a mode
// narrows the list, and picking a model yields a full, routable ID.
type Mode struct {
	ID     string // the tab's value, and the runtime: remote, local, browser
	Label  string
	Blurb  string
	Icon   func(...gsx.Attr) gsx.Node
	Ready  bool // false when this runtime cannot serve anything yet
	Fix    Fix  // read only when Ready is false
	Models []Model
}

// Fix names what is wrong and the one command that fixes it, the same contract
// the proxy's own errors keep.
type Fix struct {
	Problem string
	Detail  string
	Command string
}

// State is how usable one model is right now.
type State string

const (
	Ready       State = "ready"       // usable immediately
	Downloading State = "downloading" // arriving on this device
	Absent      State = "absent"      // one action away; Action says which
)

type Model struct {
	ID       string // full and routable, prefix included: xai/grok-4.3
	Detail   string
	State    State
	Progress float64 // percent, read only while Downloading
	Action   string  // what the button offers while Absent
	InUse    bool    // requests go here today
}

// Picker asks for the mode first, then that mode's models. Every mode's panel
// is rendered, so ui/tabs/tabs.js switches modes in the browser with no round
// trip; current only decides which panel is open at first paint.
component Picker(modes []Mode, current string) {
	<ui.Tabs value={current} class="w-full">
		<ui.TabsList class="w-full">
			{ for _, mode := range modes {
				<ui.TabsTrigger value={mode.ID} selected={mode.ID == current}>
					{ mode.Icon() }
					{ mode.Label }
				</ui.TabsTrigger>
			} }
		</ui.TabsList>
		{/* Without JavaScript the triggers above are inert buttons, so offer
		   the same choice as plain links. Nothing is lost: the picker's state
		   is in the URL either way. */}
		<noscript>
			<div class="flex flex-wrap gap-2 pt-2">
				{ for _, mode := range modes {
					<ui.Button href={ModeURL(modes, mode.ID)} variant="outline" size="sm">{ mode.Label }</ui.Button>
				} }
			</div>
		</noscript>
		{ for _, mode := range modes {
			<ui.TabsContent value={mode.ID} selected={mode.ID == current}>
				<ModePanel mode={mode}/>
			</ui.TabsContent>
		} }
	</ui.Tabs>
}

// ModePanel is one mode's body: what the mode costs you, then either its
// models or the single thing that would make the mode work.
component ModePanel(mode Mode) {
	<div class="flex flex-col gap-4 pt-4">
		<p class="text-muted-foreground">{ mode.Blurb }</p>
		{ if mode.Ready {
			<ui.ItemGroup>
				{ for _, model := range mode.Models {
					<ModelRow mode={mode.ID} model={model}/>
				} }
			</ui.ItemGroup>
		} else {
			<ui.Empty>
				<ui.EmptyHeader>
					<ui.EmptyMedia variant="icon">
						<icon.TriangleAlert/>
					</ui.EmptyMedia>
					<ui.EmptyTitle>{ mode.Fix.Problem }</ui.EmptyTitle>
					<ui.EmptyDescription>{ mode.Fix.Detail }</ui.EmptyDescription>
				</ui.EmptyHeader>
				<ui.EmptyContent>
					{/* An inline command, styled the way gsxui's own site styles one. */}
					<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-sm">{ mode.Fix.Command }</code>
				</ui.EmptyContent>
			</ui.Empty>
		} }
	</div>
}

// ModelRow is one model: what it is on the left, where it stands on the right.
component ModelRow(mode string, model Model) {
	<ui.Item variant="outline">
		<ui.ItemMedia variant="icon">
			{ switch model.State {
			case Downloading:
				<ui.Spinner/>
			case Absent:
				<icon.Download/>
			default:
				<icon.CircleCheck/>
			} }
		</ui.ItemMedia>
		<ui.ItemContent>
			<ui.ItemTitle>{ model.ID }</ui.ItemTitle>
			<ui.ItemDescription>{ model.Detail }</ui.ItemDescription>
			{ if model.State == Downloading {
				<ui.Progress value={model.Progress} class="mt-2"/>
			} }
		</ui.ItemContent>
		<ui.ItemActions>
			{ if model.InUse {
				<ui.Badge>in use</ui.Badge>
			} else {
				{ switch model.State {
				case Downloading:
					<ui.Badge variant="secondary">{ model.Percent() }</ui.Badge>
				case Absent:
					<ui.Button size="sm" variant="outline">
						<icon.Download/>
						{ model.Action }
					</ui.Button>
				default:
					<ui.Button href={UseURL(mode, model.ID)} size="sm" variant="outline">Use</ui.Button>
				} }
			} }
		</ui.ItemActions>
	</ui.Item>
}
