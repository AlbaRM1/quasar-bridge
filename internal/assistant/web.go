package assistant

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type WebResult struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitzero"`
}
type WebReader struct {
	config Config
	http   *http.Client
	lookup func(context.Context, string) ([]net.IPAddr, error)
}

func NewWebReader(c Config) *WebReader {
	w := &WebReader{config: c, lookup: net.DefaultResolver.LookupIPAddr}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // DNS validation must not be bypassed by a proxy from the environment.
	transport.DialContext = w.dial
	transport.MaxIdleConns = 2
	transport.MaxIdleConnsPerHost = 1
	transport.IdleConnTimeout = 15 * time.Second
	w.http = &http.Client{Transport: transport, Timeout: time.Duration(c.WebTimeoutMS) * time.Millisecond, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("web redirect limit exceeded")
		}
		return validateWebURL(req.URL)
	}}
	return w
}
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "2002::/16", "2001::/32", "64:ff9b::/96"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}
func validateWebURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return errors.New("web URL must be HTTP/HTTPS without credentials")
	}
	if strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), "localhost") {
		return errors.New("web private addresses are blocked")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return errors.New("web private addresses are blocked")
	}
	return nil
}
func (w *WebReader) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid web address")
	}
	ips, err := w.lookup(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("web DNS lookup failed")
	}
	for _, ip := range ips {
		a, ok := netip.AddrFromSlice(ip.IP)
		if !ok || ip.Zone != "" || !publicIP(a) {
			return nil, errors.New("web DNS resolved to a private or reserved address")
		}
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("web connection failed")
}
func (w *WebReader) Read(ctx context.Context, raw string) (WebResult, error) {
	if len(raw) > 4096 {
		return WebResult{}, errors.New("web URL exceeds limit")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return WebResult{}, errors.New("invalid web URL")
	}
	if err := validateWebURL(u); err != nil {
		return WebResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(w.config.WebTimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return WebResult{}, errors.New("invalid web URL")
	}
	req.Header.Set("User-Agent", "QuasarBridge/0.3 (text reader)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return WebResult{}, ctx.Err()
		}
		return WebResult{}, errors.New("web request failed or target blocked")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WebResult{}, errors.New("web page returned an unsuccessful HTTP status")
	}
	typ, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/html" && typ != "application/xhtml+xml" && typ != "text/plain" && typ != "application/json" {
		return WebResult{}, errors.New("web content type is not supported")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(w.config.WebMaxBytes)+1))
	if err != nil {
		return WebResult{}, errors.New("cannot read web page")
	}
	result := WebResult{URL: u.String()}
	if resp.Request != nil {
		result.URL = resp.Request.URL.String()
	}
	if len(data) > w.config.WebMaxBytes {
		data = data[:w.config.WebMaxBytes]
		result.Truncated = true
	}
	var decoder io.Reader
	if typ == "text/html" || typ == "application/xhtml+xml" {
		decoder, err = charset.NewReader(bytes.NewReader(data), resp.Header.Get("Content-Type"))
	} else if params["charset"] != "" {
		decoder, err = charset.NewReaderLabel(params["charset"], bytes.NewReader(data))
	}
	if err != nil {
		return WebResult{}, errors.New("web page charset is not supported")
	}
	if decoder != nil {
		data, err = io.ReadAll(io.LimitReader(decoder, int64(w.config.WebMaxBytes)*3+1))
		if err != nil {
			return WebResult{}, errors.New("cannot decode web page")
		}
		if len(data) > w.config.WebMaxBytes*3 {
			data = data[:w.config.WebMaxBytes*3]
			result.Truncated = true
		}
	}
	text := string(data)
	if typ == "text/html" || typ == "application/xhtml+xml" {
		result.Title, text = htmlText(data, w.config.WebMaxChars)
	}
	text = strings.Join(strings.Fields(strings.ToValidUTF8(text, "�")), " ")
	if runes := []rune(text); len(runes) > w.config.WebMaxChars {
		text = string(runes[:w.config.WebMaxChars])
		result.Truncated = true
	}
	// HTML extraction reads only enough text to establish truncation.
	if title := []rune(result.Title); len(title) > 300 {
		result.Title = string(title[:300])
	}
	result.Text = text
	if text == "" {
		return WebResult{}, errors.New("web page contains no readable text")
	}
	return result, nil
}
func htmlText(data []byte, maxChars int) (string, string) {
	z := html.NewTokenizer(bytes.NewReader(data))
	var parts []string
	var title string
	hidden := ""
	inTitle := false
	size := 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		token := z.Token()
		switch kind {
		case html.StartTagToken:
			name := token.Data
			if hidden == "" && (name == "script" || name == "style" || name == "noscript" || name == "svg") {
				hidden = name
			}
			if name == "title" {
				inTitle = true
			}
		case html.EndTagToken:
			if token.Data == hidden {
				hidden = ""
			}
			if token.Data == "title" {
				inTitle = false
			}
		case html.TextToken:
			if hidden != "" {
				continue
			}
			part := strings.Join(strings.Fields(token.Data), " ")
			if inTitle {
				title += part
				continue
			}
			if part != "" {
				parts = append(parts, part)
				size += len([]rune(part)) + 1
			}
			if size > maxChars {
				return title, strings.Join(parts, " ")
			}
		}
	}
	return title, strings.Join(parts, " ")
}
