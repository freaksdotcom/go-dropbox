package dropbox

import (
	"net/http"
	"sync"
)

// Global mutex for the process.
var mux [2]sync.Mutex
var i int

// Config for the Dropbox clients.
type Config struct {
	HTTPClient  *http.Client
	AccessToken string
	mux         *sync.Mutex
}

// NewConfig with the given access token.
func NewConfig(accessToken string) *Config {
	m := mux[i%len(mux)]
	i++
	return &Config{
		HTTPClient:  http.DefaultClient,
		AccessToken: accessToken,
		mux:         &m,
	}
}
