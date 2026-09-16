//go:build !js || !wasm

package bootstrap

import "os"

func getenv(key string) string {
	return os.Getenv(key)
}
