package probeproto

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxErrorBody is how much of an error reply the client reads to find its message
const maxErrorBody = 64 << 10

// Client speaks the protocol over net/http. Every method sends the bearer token and the instance
// id header, gzips request bodies, accepts gzipped replies, and turns a non-2xx reply into an
// *Error (except 304 on Assignments, which is (nil, nil)).
type Client struct {
	// BaseURL is the server's origin, such as https://peekaping.example.com
	BaseURL string
	// Token is the probe's token, which the server finds the probe by
	Token string
	// InstanceID is the id of this process, which stays the same until it restarts
	InstanceID string
	// HTTP makes the requests. Its timeout, if it has one, must outlast the long poll of Assignments.
	HTTP *http.Client
}

// NewClient returns a client of the server at baseURL. A nil httpClient means http.DefaultClient.
func NewClient(baseURL, token, instanceID string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, InstanceID: instanceID, HTTP: httpClient}
}

// Error is a non-2xx reply
type Error struct {
	Status  int
	Message string
	// RetryAfter is how long the server asks the probe to wait, from the Retry-After header. Zero
	// when the reply has none.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("probe server answered %d: %s", e.Status, e.Message)
}

// Hello introduces this instance to the server, which records it and answers with its own
// version and the limits the probe works within. It is the first call of every run.
func (c *Client) Hello(ctx context.Context, req *HelloRequest) (*HelloResponse, error) {
	if req == nil {
		return nil, errors.New("probeproto: nil hello request")
	}
	var response HelloResponse
	if _, err := c.do(ctx, http.MethodPost, "/hello", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// Assignments long-polls for the monitors the probe runs. version is the version of the answer
// the probe holds, empty when it holds none. The server answers when its assignments differ from
// version, or with 304 after the poll timeout, which is (nil, nil) here.
func (c *Client) Assignments(ctx context.Context, version string) (*AssignmentsResponse, error) {
	var response AssignmentsResponse
	status, err := c.do(ctx, http.MethodGet, "/assignments?"+url.Values{"version": {version}}.Encode(), nil, &response)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotModified {
		return nil, nil
	}
	return &response, nil
}

// Results sends a batch of results and answers what the server made of each
func (c *Client) Results(ctx context.Context, req *ResultsRequest) (*ResultsResponse, error) {
	if req == nil {
		return nil, errors.New("probeproto: nil results request")
	}
	var response ResultsResponse
	if _, err := c.do(ctx, http.MethodPost, "/results", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// Bye tells the server this instance is stopping, so that it takes the instance out of the dealing
// at once. The probe is not offline for it: say hello again, with a new instance id, within the
// server's PROBE_OFFLINE_AFTER.
func (c *Client) Bye(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/bye", nil, nil)
	return err
}

// do makes one call and decodes a 2xx reply into out. A reply with no body (204, 304) leaves out
// as it is. It returns the reply's status.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		compressed, err := gzipJSON(in)
		if err != nil {
			return 0, err
		}
		body = compressed
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+PathPrefix+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set(HeaderInstanceID, c.InstanceID)
	// Asking for gzip by hand turns off net/http's own decompression, so the client decodes below
	req.Header.Set("Accept-Encoding", "gzip")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Only the long poll can be answered with 304, which means "unchanged"
	notModified := resp.StatusCode == http.StatusNotModified && method == http.MethodGet
	if (resp.StatusCode < 200 || resp.StatusCode > 299) && !notModified {
		return resp.StatusCode, replyError(resp)
	}
	if resp.StatusCode == http.StatusNoContent || notModified || out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}
	reader, err := replyReader(resp)
	if err != nil {
		return resp.StatusCode, err
	}
	if err := json.NewDecoder(reader).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("probeproto: decoding the reply of %s %s: %w", method, PathPrefix+path, err)
	}
	return resp.StatusCode, nil
}

// gzipJSON marshals value and gzips it
func gzipJSON(value any) (*bytes.Reader, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

// replyReader reads the reply's body, decompressed when the server gzipped it
func replyReader(resp *http.Response) (io.Reader, error) {
	if !strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		return resp.Body, nil
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("probeproto: reading a gzipped reply: %w", err)
	}
	return zr, nil
}

// replyError turns a non-2xx reply into an *Error. The message is the API's {"message": ...}, or
// else whatever text the reply has (a proxy's, say), or else the status text.
func replyError(resp *http.Response) error {
	failure := &Error{Status: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	var raw []byte
	if reader, err := replyReader(resp); err == nil {
		raw, _ = io.ReadAll(io.LimitReader(reader, maxErrorBody))
	}
	var body ErrorResponse
	if json.Unmarshal(raw, &body) == nil && body.Message != "" {
		failure.Message = body.Message
	} else if text := strings.TrimSpace(string(raw)); text != "" && !json.Valid(raw) {
		failure.Message = text
	} else {
		failure.Message = http.StatusText(resp.StatusCode)
	}
	return failure
}

// retryAfter reads a Retry-After header, which is a number of seconds or an HTTP date. A value
// that is neither, or is already past, is zero.
func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil {
		return max(0, time.Duration(seconds)*time.Second)
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(0, time.Until(at))
	}
	return 0
}
