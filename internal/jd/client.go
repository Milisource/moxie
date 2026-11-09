// Package jd implements a headless JDownloader 2 (JD2) bridge client.
//
// # Why JD2
//
// moxie's own host resolvers (internal/downloader) stay the primary download
// path. JD2 is the fallback for hosts that gate downloads behind
// Cloudflare/Turnstile challenges or captchas: JDownloader ships maintained
// host plugins for all of moxie's target hosts (buzzheavier, datanodes,
// vikingfile, workupload, mixdrop, krakenfiles, uploadhaven, mediafire,
// gofile, hexload, pixeldrain, mega; catbox via the generic DirectHTTP
// plugin) plus an F95Zone thread decrypter.
//
// This package is a thin, opinionated wrapper around
// github.com/rkosegi/jdownloader-go, which implements the MyJDownloader
// protocol (api.jdownloader.org): AES-128-CBC request/response encryption
// with SHA-256 derived keys and HMAC-SHA256 URI signatures, session tokens,
// and device-scoped calls. The wrapper adds:
//
//   - a flat, pointer-free Download projection of the Downloads list
//   - batch-aware WaitDownloads polling with stall detection (captcha stalls)
//   - a slog→zap logging bridge so JD's debug output flows into moxie's own
//     logger instead of writing to stderr (which would corrupt the TUI)
//
// # Design decisions
//
// JD2 runs as an opt-in sidecar, never a hard dependency: a Docker container
// (jlesage/jdownloader-2) or a local headless JVM. moxie only talks to it
// through MyJDownloader's cloud API — no localhost agent, no direct JVM
// contact. See README.md in this directory for setup, cfg pre-seeding, RAM
// footprint, and failure modes (captcha stalls, 7-14 day plugin breakage
// cycles, myJD auth changes, F95Zone thread login requirement).
//
// # Usage
//
//	c := jd.New(email, password,
//	    jd.WithDeviceName("moxie"),
//	    jd.WithZapLogger(jd.NewZapFromSlog(log.Logger)),
//	)
//	if err := c.Connect(); err != nil { ... }
//	if err := c.AddLinks([]string{threadURL},
//	    jd.WithAutostart(true),
//	    jd.WithDestinationDir("/mnt/games"),
//	    jd.WithPackageName("SomeGame"),
//	); err != nil { ... }
//	done, err := c.WaitDownloads(ctx,
//	    jd.WithProgress(func(p jd.Progress) { ... }),
//	)
//
// Errors follow the project convention: fmt.Errorf("jd <op>: %w", err) with
// sentinel errors (ErrStalled) testable via errors.Is.
package jd

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rkosegi/jdownloader-go/jdownloader"
	"go.uber.org/zap"
)

const (
	// defaultAPIEndpoint is the official MyJDownloader API endpoint.
	defaultAPIEndpoint = "https://api.jdownloader.org"
	// defaultDeviceName is used when the caller does not override it.
	defaultDeviceName = "moxie"
	// defaultTimeout bounds a single HTTP call to the MyJDownloader API.
	defaultTimeout = 30 * time.Second
)

// Sentinel errors. Use errors.Is to distinguish them from transport errors.
var (
	// ErrStalled is returned by WaitDownloads when a batch makes no
	// progress for the configured stall timeout (WithStallTimeout).
	// A captcha-gated host that never gets solved is the canonical cause.
	ErrStalled = errors.New("jd: download batch stalled")
	// ErrNoDevice is returned when the configured device name does not
	// appear in the account's /my/listdevices response.
	ErrNoDevice = errors.New("jd: no such device")
)

// Client is a headless JD2 bridge bound to one MyJDownloader account and one
// device. It is safe for concurrent use once constructed.
type Client struct {
	email    string
	password string
	endpoint string
	timeout  time.Duration
	devName  string
	log      *zap.SugaredLogger

	jd     jdownloader.JdClient
	device jdownloader.Device

	mu sync.Mutex // guards device
}

// Option configures a Client. Pass to New.
type Option func(*Client)

// WithEndpoint overrides the MyJDownloader API base URL. The default is the
// official endpoint; tests and self-hosted proxies use this.
func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.endpoint = endpoint }
}

// WithDeviceName selects which of the account's JD2 instances to drive. The
// default is "moxie".
func WithDeviceName(name string) Option {
	return func(c *Client) { c.devName = name }
}

// WithTimeout bounds each HTTP request to the MyJDownloader API.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithZapLogger replaces the default no-op logger used for the underlying
// jdownloader-go client. Use NewZapFromSlog to route JD's debug output
// through moxie's slog logger (TUI-safe); for plain stderr output pass any
// zap logger, e.g. zap.NewExample().Sugar().
func WithZapLogger(l *zap.SugaredLogger) Option {
	return func(c *Client) { c.log = l }
}

// New creates a JD2 bridge client for the given MyJDownloader account. It
// performs no network I/O until Connect, AddLinks, or another method touches
// the API. The email and password are validated lazily on first use so a
// misconfigured client fails at the call site, not at construction.
func New(email, password string, opts ...Option) *Client {
	c := &Client{
		email:    email,
		password: password,
		endpoint: defaultAPIEndpoint,
		timeout:  defaultTimeout,
		devName:  defaultDeviceName,
		log:      zap.NewNop().Sugar(),
	}
	for _, o := range opts {
		o(c)
	}
	c.jd = jdownloader.NewClient(c.email, c.password, c.log,
		jdownloader.ClientOptionApiEndpoint(c.endpoint),
		jdownloader.ClientOptionTimeout(c.timeout),
	)
	return c
}

func (c *Client) validateCredentials() error {
	if strings.TrimSpace(c.email) == "" || c.password == "" {
		return errors.New("jd: email and password are required")
	}
	return nil
}

// Connect authenticates against MyJDownloader and obtains a session token.
// It is idempotent in the sense that later calls transparently reconnect
// when the session is missing or older than 30 seconds (handled by the
// underlying library), so callers only need an explicit Connect when they
// want to surface authentication errors early.
func (c *Client) Connect() error {
	if err := c.validateCredentials(); err != nil {
		return err
	}
	if err := c.jd.Connect(); err != nil {
		return fmt.Errorf("jd connect: %w", err)
	}
	return nil
}

// Disconnect invalidates the MyJDownloader session token.
func (c *Client) Disconnect() error {
	if err := c.jd.Disconnect(); err != nil {
		return fmt.Errorf("jd disconnect: %w", err)
	}
	return nil
}

// Devices lists the JD2 instances registered to the account. It is also a
// cheap health check: any error means the account credentials or the
// MyJDownloader service are unreachable.
func (c *Client) Devices() ([]DeviceInfo, error) {
	list, err := c.jd.ListDevices()
	if err != nil {
		return nil, fmt.Errorf("jd list devices: %w", err)
	}
	out := make([]DeviceInfo, 0, len(*list))
	for _, d := range *list {
		out = append(out, DeviceInfo{ID: d.Id, Type: d.Type, Name: d.Name, Status: d.Status})
	}
	return out, nil
}

// Ping verifies that the MyJDownloader account is reachable and the
// configured device exists. Returns ErrNoDevice when the device is missing.
func (c *Client) Ping() error {
	if _, err := c.resolveDevice(); err != nil {
		return err
	}
	return nil
}

// Grabber returns the LinkGrabber API of the configured device, resolving
// the device (and connecting) on first use. The raw interface is exposed so
// future wiring can reach operations the package does not wrap yet
// (Clear, Remove, RenameLink, package queries).
func (c *Client) Grabber() (jdownloader.LinkGrabber, error) {
	d, err := c.resolveDevice()
	if err != nil {
		return nil, err
	}
	return d.LinkGrabber(), nil
}

// Downloads returns the Downloads API (downloadsV2 + downloadcontroller) of
// the configured device. Exposed raw for the same reason as Grabber.
func (c *Client) Downloads() (jdownloader.Downloader, error) {
	d, err := c.resolveDevice()
	if err != nil {
		return nil, err
	}
	return d.Downloader(), nil
}

// device resolves the configured device name, caching the result. The
// underlying library auto-connects on first call, so explicit Connect is
// optional.
func (c *Client) resolveDevice() (jdownloader.Device, error) {
	if err := c.validateCredentials(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.device != nil {
		return c.device, nil
	}
	dev, err := c.jd.Device(c.devName)
	if err != nil {
		if strings.Contains(err.Error(), "no such device") {
			return nil, fmt.Errorf("%w: %s", ErrNoDevice, c.devName)
		}
		return nil, fmt.Errorf("jd device %q: %w", c.devName, err)
	}
	c.device = dev
	return dev, nil
}
