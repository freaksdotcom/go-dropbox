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
	"sync"
	"time"
)

var REQUEST_ROUTINES = 2

// Client implements a Dropbox client. You may use the Files and Users
// clients directly if preferred, however Client exposes them both.
type Client struct {
	*Config
	Users   *Users
	Files   *Files
	Sharing *Sharing
}

// New client.
func NewClient(config *Config) *Client {
	c := &Client{Config: config}
	c.Users = &Users{c}
	c.Files = &Files{c}
	c.Sharing = &Sharing{c}
	req_once.Do(func() {
		for i := 0; i < REQUEST_ROUTINES; i++ {
			go background_requests(&req_ch)
		}
	})
	return c
}

type dbx_response struct {
	body           io.ReadCloser
	content_length int64
	err            error
}

type dbx_request struct {
	request     *http.Request
	response_ch *chan *dbx_response
}

var req_ch = make(chan *dbx_request, 16)
var req_once = sync.Once{}
var shutdown_ch = make(chan bool)

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

	res_ch := make(chan *dbx_response) // Not reused so it doesn't need to be > 1
	req_ch <- &dbx_request{request: req, response_ch: &res_ch}

	res := <-res_ch

	r, err := res.body, res.err
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

	res_ch := make(chan *dbx_response) // Not reused so it doesn't need to be > 1
	req_ch <- &dbx_request{request: req, response_ch: &res_ch}

	res := <-res_ch

	return res.body, res.content_length, res.err
}

func background_requests(req_ch *chan *dbx_request) {
	for {
		select {
		case dbx_req := <-*req_ch:
			body, content_len, err := do(dbx_req.request)
			res := &dbx_response{body, content_len, err}
			*dbx_req.response_ch <- res
			close(*dbx_req.response_ch)

		case <-shutdown_ch:
			return
		}
	}
}

func retriable_request(req *http.Request) (res *http.Response, err error) {
	error_retry_time := 0.5

request_loop:
	for error_retry_time < 300 {
		var sleep_time float64
		res, err = http.DefaultClient.Do(req)
		switch {
		case res.StatusCode == 429:
			log.Printf("Received Retry status code %d.", res.StatusCode)
			if time, err := strconv.Atoi(res.Header.Get("Retry-After")); err != nil {
				log.Print("Error decoding Retry-After value")
				sleep_time = 60
			} else {
				sleep_time = float64(time)
			}
		case res.StatusCode >= 500: // Retry on 5xx
			sleep_time = error_retry_time
			error_retry_time *= 1.5
		default:
			break request_loop
		}
		log.Printf("Sleeping for %.1f seconds.", sleep_time)
		time.Sleep(time.Duration(sleep_time) * time.Second)
	}
	return
}

// perform the request.
func do(req *http.Request) (io.ReadCloser, int64, error) {
	res, err := retriable_request(req)

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
			logResponse(req, res, "Could not decode text/plain error body.")
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
