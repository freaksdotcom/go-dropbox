package dropbox

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client implements a Dropbox client. You may use the Files and Users
// clients directly if preferred, however Client exposes them both.
type Client struct {
	*Config
	Users   *Users
	Files   *Files
	Sharing *Sharing
}

// New client.
func New(config *Config) *Client {
	c := &Client{Config: config}
	c.Users = &Users{c}
	c.Files = &Files{c}
	c.Sharing = &Sharing{c}
	return c
}

// call rpc style endpoint.
func (c *Client) call(path string, in interface{}) (io.ReadCloser, error) {
	url := "https://api.dropboxapi.com/2" + path

	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	r, _, err := c.do(req)
	return r, err
}

// download style endpoint.
func (c *Client) download(path string, in interface{}, r io.Reader) (io.ReadCloser, int64, error) {
	url := "https://content.dropboxapi.com/2" + path

	body, err := json.Marshal(in)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequest("POST", url, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Dropbox-API-Arg", string(body))

	if r != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	return c.do(req)
}

func (c *Client) retriable_request(req *http.Request, backoff_start ...float32) (res *http.Response, err error) {
	var error_retry_time float32
	if len(backoff_start) > 0 {
		error_retry_time = backoff_start[0]
	}
	if error_retry_time <= 0 {
		error_retry_time = 0.5
	}

request_loop:
	for error_retry_time < 300 {
		c.Config.mux.Lock()

		var sleep_time int
		res, err = c.HTTPClient.Do(req)
		switch {
		case res.StatusCode == 429:
			log.Printf("Received Retry status code %d.", res.StatusCode)
			if time, err := strconv.Atoi(res.Header.Get("Retry-After")); err != nil {
				log.Print("Error decoding Retry-After value")
				sleep_time = 60
			} else {
				sleep_time = time
			}
		case res.StatusCode >= 500: // Retry on 5xx
			sleep_time = int(error_retry_time)
			error_retry_time *= 1.5
		default:
			c.Config.mux.Unlock()
			break request_loop
		}
		log.Printf("Sleeping for %d seconds.", sleep_time)
		time.Sleep(time.Duration(sleep_time) * time.Second)
		c.Config.mux.Unlock()
	}
	return
}

// perform the request.
func (c *Client) do(req *http.Request) (io.ReadCloser, int64, error) {
	res, err := c.retriable_request(req, 0.5)

	if err != nil {
		if b, err := ioutil.ReadAll(res.Body); err == nil {
			logResponse(req, res, string(b))
		} else {
			log.Printf("Error reading body: %s", err)

		}
		return nil, 0, err
	}

	if res.StatusCode < 400 {
		return res.Body, res.ContentLength, err
	}

	defer res.Body.Close()

	e := &Error{
		Status:     http.StatusText(res.StatusCode),
		StatusCode: res.StatusCode,
	}

	kind := res.Header.Get("Content-Type")

	if strings.Contains(kind, "text/plain") {
		if b, err := ioutil.ReadAll(res.Body); err == nil {
			e.Summary = string(b)
			logResponse(req, res, e.Summary)
			return nil, 0, e
		} else {
			return nil, 0, err
		}
	}

	if err := json.NewDecoder(res.Body).Decode(e); err != nil {
		logResponse(req, res, e.Summary)
		return nil, 0, err
	}
	return nil, 0, e
}

func logResponse(req *http.Request, res *http.Response, body string) {
	if res.StatusCode != 409 {
		log.Printf("URL: %s - Method: %s", req.URL, req.Method)
		log.Printf("HTTP StatusCode %d", res.StatusCode)
		log.Print(body)

	}
}
