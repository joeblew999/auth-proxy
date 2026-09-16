package main

import (
	"github.com/gsxhq/gsx"
	"github.com/gsxhq/vite"

	"github.com/joeblew999/grok-oauth-proxy/cmd/gui/ui"
	"github.com/joeblew999/grok-oauth-proxy/cmd/gui/views"
)

component Layout(title string, children gsx.Node) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="UTF-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
			<title>{ title }</title>
			<script>
				// Theme init, the way gsxui's own site does it: gsxui's dark
				// palette hangs off a `dark` class on <html>, not off the media
				// query, so something has to set it. A blocking head script
				// runs before first paint, so dark never flashes light. A
				// stored choice wins; otherwise follow the OS.
				try {
					var gsxuiTheme = localStorage.getItem("gsxui-theme");
					if (gsxuiTheme === "dark" || (!gsxuiTheme && matchMedia("(prefers-color-scheme: dark)").matches)) {
						document.documentElement.classList.add("dark");
					}
				} catch (e) {}
			</script>
			{{ v := vite.FromContext(ctx) }}
			{ if v.Dev() {
				<style>
					html[data-loading] body {
						visibility: hidden;
					}

					html[data-loading] * {
						transition: none !important;
					}
				</style>
				<script>
					// Dev-only FOUC gate. Vite injects CSS via JS after the HTML
					// loads, so hide the page until every module script has run
					// (DOMContentLoaded) and one paint has landed (double rAF),
					// then reveal. Prod ships real <link rel=stylesheet> tags
					// below, so no gate is emitted there.
					document.documentElement.dataset.loading = "true";
					var unhide = function () {
						document.documentElement.removeAttribute("data-loading");
					};
					var reveal = function () {
						requestAnimationFrame(function () { requestAnimationFrame(unhide); });
					};
					if (document.readyState === "loading") {
						document.addEventListener("DOMContentLoaded", reveal);
					} else {
						reveal();
					}
					// Safety net (rAF pauses in background tabs).
					setTimeout(unhide, 5000);
				</script>
			} }
			{{ assets := v.Entry("web/main.js") }}
			{ for _, href := range assets.CSS {
				<link rel="stylesheet" href={href}/>
			} }
			{ for _, src := range assets.Preloads {
				<link rel="modulepreload" href={src}/>
			} }
			{ for _, src := range assets.JS {
				<script type="module" src={src}></script>
			} }
		</head>
		<body>{ children }</body>
	</html>
}

component Index(title string, modes []views.Mode, current string) {
	<Layout title={title}>
		<main id="app" class="mx-auto flex w-full max-w-2xl flex-col gap-8 p-8">
			<header class="flex flex-col gap-2">
				<h1 class="text-3xl font-semibold tracking-tight">{ title }</h1>
				<p class="text-muted-foreground">
					A model's prefix names its provider, and the provider says where it runs. So the question is where first, then
					which model.
				</p>
			</header>
			<views.Picker modes={modes} current={current}/>
			<Scaffold/>
		</main>
	</Layout>
}

// Scaffold keeps the starter's own round trips on the page, because each one
// proves a link in the chain: /public serves static files, web/counter.js is
// bundled by Vite and runs in the browser, and gsx dev is live.
component Scaffold() {
	<footer class="flex flex-col items-center gap-4 border-t pt-8">
		<div class="flex items-center gap-8">
			<a href="https://vite.dev" target="_blank" rel="noreferrer">
				<img src="/public/vite.svg" class="h-10 transition hover:opacity-70" alt="Vite logo"/>
			</a>
			<a href="https://github.com/gsxhq/gsx" target="_blank" rel="noreferrer">
				<img src="/public/gsx.svg" class="h-10 transition hover:opacity-70" alt="gsx logo"/>
			</a>
		</div>
		<ui.Button id="counter" variant="outline" size="sm">count is 0</ui.Button>
		<p class="text-center text-sm text-muted-foreground">
			Edit <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-sm">app.gsx</code> and save — the page
			live-reloads.
		</p>
		{{ v := vite.FromContext(ctx) }}
		{ if v.Dev() {
			<p class="text-center text-sm text-muted-foreground">
				Press <ui.Kbd>Cmd-D</ui.Kbd> for the gsx dev panel — <ui.Kbd>Ctrl-D</ui.Kbd> on Windows and Linux.
			</p>
		} }
	</footer>
}
