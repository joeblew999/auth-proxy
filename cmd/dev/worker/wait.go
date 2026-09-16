package worker

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// stableFor is how many polls in a row must answer 200 before a Worker counts
// as up: a workers.dev hostname that has just been (re)deployed answers on one
// edge and Cloudflare error 1042 on the next for a short while, so a single
// success proves nothing.
const stableFor = 3

// Wait polls url until it answers 200 stableFor times in a row.
func Wait(out io.Writer, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	streak := 0
	for {
		resp, err := httpClient.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				streak++
				if streak == stableFor {
					fmt.Fprintf(out, "%s is up\n", url)
					return nil
				}
				sleep(2 * time.Second)
				continue
			}
		}
		streak = 0
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not answer 200 steadily within %s; look at its logs (dev logs DIR)", url, timeout)
		}
		fmt.Fprintf(out, "waiting for %s to come online...\n", url)
		sleep(5 * time.Second)
	}
}
