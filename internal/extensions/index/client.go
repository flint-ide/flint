package index

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// indexTimeout bounds each of the two small requests (index, signature).
	indexTimeout = 20 * time.Second
	// downloadTimeout bounds an archive download.
	downloadTimeout = 10 * time.Minute
	maxRedirects    = 5
)

// Client downloads an index and its archives: anonymous GETs with no
// cookies and no identifiers beyond the User-Agent "Flint/<version>", https
// only (http for loopback), bounded in size and time. Nothing here runs on
// its own: the callers are user actions.
type Client struct {
	// Version is sent as User-Agent "Flint/<Version>".
	Version string
	// Transport is the HTTP transport; nil uses http.DefaultTransport. Tests
	// pass an httptest server's.
	Transport http.RoundTripper
}

// httpClient has no cookie jar. A redirect is followed (GitHub's release
// assets redirect to its storage host) only to an address CheckURL accepts,
// and never from https to http.
func (c *Client) httpClient() *http.Client {
	return &http.Client{
		Transport: c.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if err := checkParsedURL(req.URL); err != nil {
				return fmt.Errorf("redirect refused: %w", err)
			}
			if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return errors.New("redirect refused: from https to http")
			}
			return nil
		},
	}
}

func (c *Client) get(ctx context.Context, rawURL string) (*http.Response, error) {
	if err := CheckURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Flint/"+c.Version)
	req.Header.Set("Accept", "application/octet-stream, application/json;q=0.9, */*;q=0.1")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%s was not found (404)", req.URL.Redacted())
		}
		return nil, fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	return resp, nil
}

// getSmall reads a whole response of at most limit bytes.
func (c *Client) getSmall(ctx context.Context, rawURL string, limit int64, what string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, indexTimeout)
	defer cancel()
	resp, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d KiB", what, limit>>10)
	}
	return data, nil
}

// Fetched is a downloaded, verified index with the bytes it was read from.
type Fetched struct {
	Index *Index
	// Data and Signature are the files as downloaded (to cache and verify
	// again later).
	Data, Signature []byte
}

// Fetch downloads the index at indexURL and its signature (indexURL +
// SignatureSuffix), verifies the signature with key, then parses the index.
// Without a key nothing is requested: ErrNoKey.
func (c *Client) Fetch(ctx context.Context, indexURL string, key ed25519.PublicKey) (*Fetched, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, ErrNoKey
	}
	data, err := c.getSmall(ctx, indexURL, MaxIndexBytes, "the extension index")
	if err != nil {
		return nil, err
	}
	sig, err := c.getSmall(ctx, indexURL+SignatureSuffix, MaxSignatureBytes, "the extension index's signature")
	if err != nil {
		return nil, fmt.Errorf("the extension index has no signature Flint could download, so it was not used: %w", err)
	}
	ix, err := Open(data, sig, key)
	if err != nil {
		return nil, err
	}
	return &Fetched{Index: ix, Data: data, Signature: sig}, nil
}

// Open verifies sig over data with key, then parses the index.
func Open(data, sig []byte, key ed25519.PublicKey) (*Index, error) {
	if err := Verify(key, data, sig); err != nil {
		return nil, err
	}
	return Parse(data)
}

// Download writes the archive a to the new file dst. It fails, leaving no
// file, when the response is not exactly a.Size bytes (at most
// MaxArchiveBytes) or its SHA-256 is not a.SHA256.
func (c *Client) Download(ctx context.Context, a Archive, dst string) (err error) {
	if err := a.check(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	resp, err := c.get(ctx, a.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > a.Size {
		return fmt.Errorf("the download is larger (%d bytes) than the index says (%d bytes); nothing was installed", resp.ContentLength, a.Size)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, a.Size+1))
	if err != nil {
		return fmt.Errorf("the download stopped: %w", err)
	}
	if n != a.Size {
		return fmt.Errorf("the download is not the size the index says (%d bytes); nothing was installed", a.Size)
	}
	if !bytes.Equal(h.Sum(nil), a.hash()) {
		return errors.New("the download's SHA-256 is not the one in the signed index: the file was changed or replaced; nothing was installed")
	}
	return nil
}
