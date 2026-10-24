// Package extensionbridge lets the user's own browser resolve F95Zone's
// /dl/ check pages, where hCaptcha passes normally, and hands the resulting
// host URL back to moxie over a loopback HTTP callback.
//
// Flow:
//
//	moxie starts a loopback listener on an ephemeral port and opens
//	https://f95zone.to/dl/…#moxie=<port>:<token> in the default browser
//	→ the moxie-bridge extension notices the fragment on the /dl/ page,
//	  watches that tab and POSTs the destination URL to 127.0.0.1:<port>
//	→ moxie validates the single-use token and downloads the URL itself.
//
// The fragment is never sent to F95Zone (browsers do not transmit
// fragments), and the listener only accepts the random token minted for the
// pending resolve, so an unrelated page cannot inject a download URL.
package extensionbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout bounds a resolve when the caller's context has no deadline.
const DefaultTimeout = 10 * time.Minute

// Bridge is a loopback callback server for the moxie-bridge extension.
type Bridge struct {
	ln   net.Listener
	port int
	srv  *http.Server
	open func(string) error

	mu    sync.Mutex
	token string      // token accepted for the pending resolve ("" = none)
	ch    chan string // receives the validated destination URL
}

// Option configures a Bridge (used by tests; production uses the defaults).
type Option func(*Bridge)

// WithOpener replaces the browser opener (default: desktop.BrowserOpenURL
// equivalent, supplied by the caller). Tests use it to capture the URL.
func WithOpener(fn func(string) error) Option {
	return func(b *Bridge) { b.open = fn }
}

// New starts the loopback listener. The port is ephemeral; it is embedded in
// the fragment moxie opens, so the extension never needs a fixed port.
func New(opts ...Option) (*Bridge, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("extensionbridge: listen: %w", err)
	}
	b := &Bridge{
		ln:   ln,
		port: ln.Addr().(*net.TCPAddr).Port,
		ch:   make(chan string, 1),
		open: func(rawURL string) error { return fmt.Errorf("extensionbridge: no opener configured (url %s)", rawURL) },
	}
	for _, o := range opts {
		o(b)
	}
	b.srv = &http.Server{Handler: http.HandlerFunc(b.handle), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = b.srv.Serve(ln) }()
	return b, nil
}

// Port returns the loopback port the extension should call.
func (b *Bridge) Port() int { return b.port }

// Close stops the listener.
func (b *Bridge) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return b.srv.Shutdown(ctx)
}

// Resolve opens targetURL in the browser, tagged with a single-use token, and
// waits for the extension to report the destination URL.
func (b *Bridge) Resolve(ctx context.Context, targetURL string) (string, error) {
	if b.open == nil {
		return "", fmt.Errorf("extensionbridge: no browser opener configured")
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}

	b.mu.Lock()
	b.token = token
	b.mu.Unlock()
	// Drain a stale callback from a previous, aborted resolve.
	select {
	case <-b.ch:
	default:
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		return "", fmt.Errorf("extensionbridge: invalid URL: %w", err)
	}
	u.Fragment = fmt.Sprintf("moxie=%d:%s", b.port, token)

	if err := b.open(u.String()); err != nil {
		return "", fmt.Errorf("extensionbridge: open browser: %w", err)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	select {
	case dest := <-b.ch:
		return dest, nil
	case <-ctx.Done():
		return "", fmt.Errorf("extensionbridge: timed out waiting for the browser check (is the moxie-bridge extension installed and enabled?): %w", ctx.Err())
	}
}

// resolveRequest is the JSON body the extension POSTs back.
type resolveRequest struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

func (b *Bridge) handle(w http.ResponseWriter, r *http.Request) {
	// The extension's service worker may run from a chrome-extension:// or
	// moz-extension:// origin; allow it explicitly. The token is the actual
	// authorization, not CORS.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/resolved" {
		http.NotFound(w, r)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body")
		return
	}
	var req resolveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	b.mu.Lock()
	want := b.token
	b.mu.Unlock()
	if want == "" || req.Token != want {
		writeError(w, http.StatusForbidden, "token mismatch")
		return
	}
	dest := strings.TrimSpace(req.URL)
	if !isAcceptableDestination(dest) {
		writeError(w, http.StatusBadRequest, "destination must be an http(s) URL outside f95zone.to")
		return
	}

	b.mu.Lock()
	b.token = "" // single use
	b.mu.Unlock()

	select {
	case b.ch <- dest:
	default: // a resolve is already satisfied
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// isAcceptableDestination rejects empty and F95Zone-internal URLs: the
// extension must report the host page, not an intermediate check page.
func isAcceptableDestination(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host != "f95zone.to" && !strings.HasSuffix(host, ".f95zone.to")
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "msg": msg})
}

func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("extensionbridge: token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
