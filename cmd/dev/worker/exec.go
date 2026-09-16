package worker

import (
	"net/http"
	"time"
)

// The network seams, as variables so tests can replace them.
var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	sleep      = time.Sleep
)
