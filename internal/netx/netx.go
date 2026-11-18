// Package netx has the HTTP helpers: JSON requests and cancellable streaming downloads.
package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/app"
)

const chunk = 256 * 1024

// ErrCancelled is returned when the user cancels a running operation.
var ErrCancelled = errors.New("cancelled")

// HTTPError is a response with a status other than 2xx.
type HTTPError struct {
	Code int
	URL  string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP Error %d: %s", e.Code, http.StatusText(e.Code))
}

// IsStatus reports whether err is an HTTPError with this status code.
func IsStatus(err error, code int) bool {
	var h *HTTPError
	return errors.As(err, &h) && h.Code == code
}

// Progress receives (done bytes, total bytes or 0).
type Progress func(done, total int64)

func newRequest(ctx context.Context, method, url string, headers map[string]string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", app.UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func do(req *http.Request, timeout time.Duration) (*http.Response, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, &HTTPError{resp.StatusCode, req.URL.String()}
	}
	return resp, nil
}

// GetBytes fetches url's body.
func GetBytes(url string, headers map[string]string) ([]byte, error) {
	req, err := newRequest(context.Background(), "GET", url, headers, nil)
	if err != nil {
		return nil, err
	}
	resp, err := do(req, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// GetJSON decodes url's JSON body into out.
func GetJSON(url string, headers map[string]string, out any) error {
	b, err := GetBytes(url, headers)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// PostJSON posts body as JSON and decodes the answer into out.
func PostJSON(url string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := newRequest(context.Background(), "POST", url, map[string]string{"Content-Type": "application/json"}, data)
	if err != nil {
		return err
	}
	resp, err := do(req, 30*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// Download saves url to dest. It writes to dest + ".part" and renames on success, so a cancelled or
// failed download never leaves a file that looks complete. cancel may be nil.
func Download(url, dest string, progress Progress, cancel <-chan struct{}, headers map[string]string) (string, error) {
	abs, _ := filepath.Abs(dest)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	part := dest + ".part"
	defer os.Remove(part)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if cancel != nil {
		go func() {
			select {
			case <-cancel:
				stop()
			case <-ctx.Done():
			}
		}()
	}
	req, err := newRequest(ctx, "GET", url, headers, nil)
	if err != nil {
		return "", err
	}
	// No overall timeout: big downloads take long. A connection that sends nothing for stallTime is
	// given up (the watchdog below); the transport times out headers and TLS.
	client := &http.Client{Transport: downloadTransport}
	var stalled atomic.Bool
	watchdog := time.AfterFunc(stallTime, func() { stalled.Store(true); stop() })
	defer watchdog.Stop()
	resp, err := client.Do(req)
	if err != nil {
		if isCancelled(cancel) {
			return "", ErrCancelled
		}
		if stalled.Load() {
			return "", ErrStalled
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", &HTTPError{resp.StatusCode, url}
	}
	total, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	buf := make([]byte, chunk)
	var done int64
	last := time.Time{}
	for {
		if isCancelled(cancel) {
			f.Close()
			return "", ErrCancelled
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return "", werr
			}
			done += int64(n)
			watchdog.Reset(stallTime)
			if progress != nil && time.Since(last) > 200*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			if isCancelled(cancel) {
				return "", ErrCancelled
			}
			if stalled.Load() {
				return "", ErrStalled
			}
			return "", rerr
		}
	}
	if progress != nil {
		progress(done, total)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// stallTime is how long a download may receive nothing before it is given up.
const stallTime = 60 * time.Second

// ErrStalled: the server stopped sending.
var ErrStalled = errors.New("The download stopped: nothing arrived for a minute. Try again.")

// downloadTransport is shared by every download, so idle connections are reused and closed after a while.
var downloadTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 60 * time.Second,
	TLSHandshakeTimeout:   60 * time.Second,
	IdleConnTimeout:       90 * time.Second,
}

func isCancelled(cancel <-chan struct{}) bool {
	if cancel == nil {
		return false
	}
	select {
	case <-cancel:
		return true
	default:
		return false
	}
}
