package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

type Options struct {
	ProxyURL      string        // optional: http or https
	UserAgent     string        // optional
	Timeout       time.Duration // total request timeout
	MaxBodyBytes  int64         // limit response size
	MaxIdleConns  int           // pool size
	IdleConnTTL   time.Duration
	HeaderTimeout time.Duration
}

func New(opts Options) (*http.Client, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 2 << 20 // 2 MiB
	}
	if opts.MaxIdleConns <= 0 {
		opts.MaxIdleConns = 100
	}
	if opts.IdleConnTTL <= 0 {
		opts.IdleConnTTL = 90 * time.Second
	}
	if opts.HeaderTimeout <= 0 {
		opts.HeaderTimeout = 10 * time.Second
	}

	var proxy func(*http.Request) (*url.URL, error)
	if opts.ProxyURL != "" {
		u, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, err
		}
		// Allow only http, https
		switch u.Scheme {
		case "http", "https":
			// OK
		default:
			return nil, errors.New("unsupported proxy scheme")
		}
		proxy = http.ProxyURL(u)
	} else {
		proxy = http.ProxyFromEnvironment
	}

	transport := &http.Transport{
		Proxy: proxy,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxConnsPerHost:       64,
		MaxIdleConns:          opts.MaxIdleConns,
		IdleConnTimeout:       opts.IdleConnTTL,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: opts.HeaderTimeout,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &http.Client{
		Timeout:   opts.Timeout,
		Transport: transport,
	}, nil
}

// Do performs the request with context, applies a body size limit,
// and ensures the body is closed on all paths.
func Do(ctx context.Context, client *http.Client, req *http.Request, maxBody int64) (*http.Response, []byte, error) {
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if maxBody <= 0 {
		maxBody = 2 << 20 // 2 MiB
	}
	lr := io.LimitedReader{R: resp.Body, N: maxBody}
	body, err := io.ReadAll(&lr)
	if err != nil {
		return resp, nil, err
	}
	// If N == 0, we hit the limit
	if lr.N == 0 {
		return resp, body, errors.New("response body too large")
	}
	return resp, body, nil
}
