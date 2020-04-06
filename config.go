package dropbox

import (
	"net/http"
	"sync"
)

// Global mutex for the process.
var mux sync.Mutex

// Config for the Dropbox clients.
type Config struct {
	HTTPClient  *http.Client
	AccessToken string
	mux         *sync.Mutex
}

// NewConfig with the given access token.
func NewConfig(accessToken string) *Config {
	return &Config{
		HTTPClient:  http.DefaultClient,
		AccessToken: accessToken,
		mux:         &mux,
	}
}
