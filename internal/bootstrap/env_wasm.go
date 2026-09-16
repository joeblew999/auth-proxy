//go:build js && wasm

package bootstrap

import "github.com/syumai/workers-go/cloudflare"

func getenv(key string) string {
	return cloudflare.Getenv(key)
}
