package jd

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rkosegi/jdownloader-go/jdownloader"
)

// ---------------------------------------------------------------------------
// mockMyJD: a scriptable stand-in for api.jdownloader.org that implements
// the real protocol — AES-128-CBC encryption (first 16 bytes of a SHA-256
// derived key are the IV, last 16 the AES key), base64 transport, and
// HMAC-SHA256 URI signatures, including the session-token key rotation on
// /my/connect. It decrypts and verifies everything the client sends, which
// is what makes these tests meaningful: a request the mock cannot decrypt
// or verify fails loudly.
// ---------------------------------------------------------------------------

// apiError is a plaintext (non-encrypted) API error response.
type apiError struct {
	status int
	msg    string
}

type mockMyJD struct {
	t       *testing.T
	mu      sync.Mutex
	server  *httptest.Server
	devices []jdownloader.DeviceInfo

	email string // lowercase, as the client sends it
	pass  string

	loginSecret  [32]byte
	serverToken  [32]byte // rotated after connect
	deviceToken  [32]byte // rotated after connect
	sessionToken string

	connectErr *apiError

	gotConnectEmail  string
	gotConnectAppKey string
	gotAddParams     *jdownloader.AddLinksParams
	calls            []string // device actions received, in order

	// queryLinksQueue, when non-nil, serves one scripted response per
	// /downloadsV2/queryLinks poll (FIFO). queryLinks is the static
	// fallback served when the queue is empty or unset.
	queryLinks      []jdownloader.DownloadLink
	queryLinksQueue [][]jdownloader.DownloadLink
}

func newMockMyJD(t *testing.T, email, pass string) *mockMyJD {
	t.Helper()
	m := &mockMyJD{
		t:           t,
		email:       strings.ToLower(email),
		pass:        pass,
		loginSecret: sha256.Sum256([]byte(strings.ToLower(email) + pass + "server")),
		devices: []jdownloader.DeviceInfo{
			{Id: "dev-001", Type: "JDownloader", Name: "other-box", Status: "online"},
			{Id: "dev-002", Type: "JDownloader", Name: "jd-device", Status: "online"},
		},
	}
	m.deviceToken = sha256.Sum256([]byte(strings.ToLower(email) + pass + "device"))
	m.serverToken = m.loginSecret
	m.server = httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockMyJD) url() string { return m.server.URL }

func (m *mockMyJD) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch r.URL.Path {
	case "/my/connect":
		m.handleConnect(w, r)
	case "/my/listdevices":
		m.handleListDevices(w, r)
	default:
		m.handleDeviceCall(w, r)
	}
}

func (m *mockMyJD) handleConnect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	m.gotConnectEmail = q.Get("email")
	m.gotConnectAppKey = q.Get("appkey")
	if m.connectErr != nil {
		writePlainJSON(w, m.connectErr.status, map[string]string{"error": m.connectErr.msg})
		return
	}
	m.verifySignature(w, r, m.loginSecret)

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		m.t.Errorf("connect: rand failed: %v", err)
	}
	m.sessionToken = hex.EncodeToString(raw)
	m.serverToken = updateToken(m.loginSecret, raw)
	m.deviceToken = updateToken(deviceSecret(m.email, m.pass), raw)

	writeEncrypted(w, m.loginSecret, map[string]any{
		"sessiontoken": m.sessionToken,
		"regaintoken":  m.sessionToken,
		"rid":          mustInt(q.Get("rid")),
	})
}

func (m *mockMyJD) handleListDevices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	m.verifySignature(w, r, m.serverToken)
	if got := q.Get("sessiontoken"); got != m.sessionToken {
		m.t.Errorf("listdevices: sessiontoken = %q, want %q", got, m.sessionToken)
	}
	writeEncrypted(w, m.serverToken, map[string]any{
		"rid":  mustInt(q.Get("rid")),
		"list": m.devices,
	})
}

func (m *mockMyJD) handleDeviceCall(w http.ResponseWriter, r *http.Request) {
	action := deviceAction(r.URL.Path)
	if action == "" {
		m.t.Errorf("device call: cannot parse action from path %q", r.URL.Path)
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	// The path embeds the session token; assert it matches so the
	// device-scoped routing is actually exercised.
	if !strings.Contains(r.URL.Path, m.sessionToken) {
		m.t.Errorf("device call: path %q does not contain session token", r.URL.Path)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.t.Errorf("device call: read body: %v", err)
		return
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		m.t.Errorf("device call %s: body is not base64: %v", action, err)
		return
	}
	plain, err := decrypt(ct, m.deviceToken)
	if err != nil {
		m.t.Errorf("device call %s: decrypt failed: %v", action, err)
		return
	}
	var req actionRequest
	if err := json.Unmarshal(plain, &req); err != nil {
		m.t.Errorf("device call %s: unmarshal request: %v", action, err)
		return
	}
	m.calls = append(m.calls, req.URL)

	switch req.URL {
	case "/linkgrabberv2/addLinks":
		if len(req.Params) == 0 {
			m.t.Errorf("addLinks: no params")
			return
		}
		raw, ok := req.Params[0].(string)
		if !ok {
			m.t.Errorf("addLinks: param 0 is %T, want JSON string", req.Params[0])
			return
		}
		p := &jdownloader.AddLinksParams{}
		if err := json.Unmarshal([]byte(raw), p); err != nil {
			m.t.Errorf("addLinks: unmarshal params: %v", err)
			return
		}
		m.gotAddParams = p
		writeEncrypted(w, m.deviceToken, map[string]any{
			"data": map[string]any{},
			"rid":  req.RequestID,
			"src":  "linkgrabberv2",
			"type": "addLinks",
		})
	case "/downloadsV2/queryLinks":
		resp := m.queryLinks
		if len(m.queryLinksQueue) > 0 {
			resp = m.queryLinksQueue[0]
			m.queryLinksQueue = m.queryLinksQueue[1:]
		}
		writeEncrypted(w, m.deviceToken, map[string]any{
			"data": resp,
			"rid":  req.RequestID,
		})
	default:
		m.t.Errorf("device call: unexpected action %q", req.URL)
		http.Error(w, "unexpected action", http.StatusNotFound)
	}
}

// verifySignature recomputes the HMAC-SHA256 signature over the raw request
// URI (minus the signature parameter) and fails the test on mismatch. The
// client always appends signature last, so trimming the suffix is exact.
func (m *mockMyJD) verifySignature(w http.ResponseWriter, r *http.Request, key [32]byte) {
	sig := r.URL.Query().Get("signature")
	signed := strings.TrimSuffix(r.URL.RequestURI(), "&signature="+sig)
	want := signString(signed, key[:])
	if !hmac.Equal([]byte(want), []byte(sig)) {
		m.t.Errorf("%s: signature mismatch\n  got  %q\n  want %q\n  over %q", r.URL.Path, sig, want, signed)
	}
}

// ---------------------------------------------------------------------------
// Protocol helpers (mirror the jdownloader-go crypto — it is unexported
// there, so the mock reimplements the documented scheme).
// ---------------------------------------------------------------------------

type actionRequest struct {
	URL        string        `json:"url"`
	Params     []interface{} `json:"params"`
	RequestID  int64         `json:"rid"`
	ApiVersion int           `json:"apiVer"`
}

func deviceSecret(email, pass string) [32]byte {
	return sha256.Sum256([]byte(email + pass + "device"))
}

func updateToken(secret [32]byte, session []byte) [32]byte {
	var buf bytes.Buffer
	buf.Write(secret[:])
	buf.Write(session)
	return sha256.Sum256(buf.Bytes())
}

func signString(s string, key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func encrypt(plaintext []byte, secret [32]byte) ([]byte, error) {
	block, err := aes.NewCipher(secret[16:])
	if err != nil {
		return nil, err
	}
	padding := block.BlockSize() - len(plaintext)%block.BlockSize()
	padded := append(plaintext, bytes.Repeat([]byte{byte(padding)}, padding)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, secret[:16]).CryptBlocks(out, padded)
	return out, nil
}

func decrypt(ct []byte, secret [32]byte) ([]byte, error) {
	block, err := aes.NewCipher(secret[16:])
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, secret[:16]).CryptBlocks(plain, ct)
	n := int(plain[len(plain)-1])
	if n < 1 || n > block.BlockSize() || n > len(plain) {
		return nil, errors.New("invalid padding")
	}
	return plain[:len(plain)-n], nil
}

func writeEncrypted(w http.ResponseWriter, secret [32]byte, v any) {
	plain, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	ct, err := encrypt(plain, secret)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/aesjson-jd; charset=utf-8")
	_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(ct)))
}

func writePlainJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// deviceAction extracts the API action from a device-scoped path of the
// form /t_<sessiontoken>_<deviceid>/linkgrabberv2/addLinks.
func deviceAction(path string) string {
	rest := strings.TrimPrefix(path, "/t_")
	i := strings.Index(rest, "_")
	if i < 0 {
		return ""
	}
	rest = rest[i+1:]
	j := strings.Index(rest, "/")
	if j < 0 {
		return ""
	}
	return rest[j:]
}

func mustInt(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestClient(t *testing.T, m *mockMyJD) *Client {
	t.Helper()
	return New("Test@Example.com", "hunter2-secret",
		WithEndpoint(m.url()),
		WithDeviceName("jd-device"),
	)
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func TestConnect(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)

	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gotConnectEmail != "test@example.com" {
		t.Errorf("connect email = %q, want lowercased account email", m.gotConnectEmail)
	}
	if m.gotConnectAppKey == "" {
		t.Errorf("connect appkey is empty")
	}
}

func TestConnectRejectsEmptyCredentials(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := New("", "", WithEndpoint(m.url()))

	err := c.Connect()
	if err == nil || !strings.Contains(err.Error(), "email and password") {
		t.Fatalf("Connect with empty credentials: err = %v, want credential error", err)
	}
}

func TestConnectAuthFailure(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	m.connectErr = &apiError{status: http.StatusForbidden, msg: "Authentication failed"}
	c := newTestClient(t, m)

	err := c.Connect()
	if err == nil {
		t.Fatal("Connect: expected error for bad credentials, got nil")
	}
	if !strings.Contains(err.Error(), "Authentication failed") {
		t.Errorf("Connect error = %q, want server message surfaced", err)
	}
}

func TestDevices(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)

	devs, err := c.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("Devices: got %d entries, want 2", len(devs))
	}
	if devs[1].Name != "jd-device" || devs[1].ID != "dev-002" {
		t.Errorf("Devices[1] = %+v, want the jd-device entry", devs[1])
	}
}

func TestDeviceNotFound(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := New("test@example.com", "hunter2-secret",
		WithEndpoint(m.url()),
		WithDeviceName("ghost-box"),
	)

	err := c.AddLinks([]string{"https://example.com/file.zip"})
	if !errors.Is(err, ErrNoDevice) {
		t.Fatalf("AddLinks: err = %v, want ErrNoDevice", err)
	}
	if err == nil || !strings.Contains(err.Error(), "ghost-box") {
		t.Errorf("AddLinks error = %v, want device name in message", err)
	}
}

func TestPing(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)
	if err := c.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AddLinks
// ---------------------------------------------------------------------------

func TestAddLinks(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)

	err := c.AddLinks(
		[]string{"https://host-a.example/file1.zip", "https://host-b.example/file2.zip"},
		WithAutostart(true),
		WithDestinationDir("/mnt/games/SomeGame"),
		WithPackageName("SomeGame"),
	)
	if err != nil {
		t.Fatalf("AddLinks: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gotAddParams == nil {
		t.Fatal("AddLinks: mock never received addLinks call")
	}
	if m.gotAddParams.Links != "https://host-a.example/file1.zip,https://host-b.example/file2.zip" {
		t.Errorf("AddLinks links = %q, want comma-joined input", m.gotAddParams.Links)
	}
	if m.gotAddParams.Autostart == nil || !*m.gotAddParams.Autostart {
		t.Errorf("AddLinks autostart = %v, want true (default)", m.gotAddParams.Autostart)
	}
	if m.gotAddParams.DestinationFolder == nil || *m.gotAddParams.DestinationFolder != "/mnt/games/SomeGame" {
		t.Errorf("AddLinks destinationFolder = %v, want /mnt/games/SomeGame", m.gotAddParams.DestinationFolder)
	}
	if m.gotAddParams.PackageName == nil || *m.gotAddParams.PackageName != "SomeGame" {
		t.Errorf("AddLinks packageName = %v, want SomeGame", m.gotAddParams.PackageName)
	}
}

// TestAddLinksImplicitConnect verifies the flow works without an explicit
// Connect: the underlying library connects lazily on device resolution.
func TestAddLinksImplicitConnect(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)

	if err := c.AddLinks([]string{"https://host.example/file.zip"}); err != nil {
		t.Fatalf("AddLinks: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gotConnectEmail == "" {
		t.Error("AddLinks did not trigger implicit connect")
	}
}

func TestAddLinksAutostartFalse(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)

	if err := c.AddLinks([]string{"https://host.example/file.zip"}, WithAutostart(false)); err != nil {
		t.Fatalf("AddLinks: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gotAddParams.Autostart == nil || *m.gotAddParams.Autostart {
		t.Errorf("AddLinks autostart = %v, want false", m.gotAddParams.Autostart)
	}
}

func TestAddLinksNoURLs(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)

	if err := c.AddLinks([]string{"  ", ""}); err == nil {
		t.Fatal("AddLinks with blank URLs: expected error, got nil")
	} else if !strings.Contains(err.Error(), "no URLs") {
		t.Errorf("AddLinks error = %v, want no-URLs error", err)
	}
}

// ---------------------------------------------------------------------------
// QueryDownloads
// ---------------------------------------------------------------------------

func TestQueryDownloads(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	m.queryLinks = []jdownloader.DownloadLink{
		{
			Uuid:        ptr(int64(1001)),
			PackageUuid: ptr(int64(2001)),
			Name:        ptr("game-part1.zip"),
			Url:         ptr("https://host-a.example/file1.zip"),
			Host:        ptr("host-a.example"),
			Status:      ptr("Running"),
			BytesTotal:  ptr(int64(1024)),
			BytesLoaded: ptr(int64(512)),
			Speed:       ptr(float64(2048)),
			Finished:    ptr(false),
			Enabled:     ptr(true),
		},
		{
			Uuid:        ptr(int64(1002)),
			Name:        ptr("game-part2.zip"),
			Url:         ptr("https://host-b.example/file2.zip"),
			Host:        ptr("host-b.example"),
			Status:      ptr("Finished"),
			BytesTotal:  ptr(int64(2048)),
			BytesLoaded: ptr(int64(2048)),
			Finished:    ptr(true),
			Enabled:     ptr(true),
		},
	}
	c := newTestClient(t, m)

	dl, err := c.QueryDownloads()
	if err != nil {
		t.Fatalf("QueryDownloads: %v", err)
	}
	if len(dl) != 2 {
		t.Fatalf("QueryDownloads: got %d links, want 2", len(dl))
	}
	first := dl[0]
	if first.Name != "game-part1.zip" || first.BytesLoaded != 512 || first.Speed != 2048 {
		t.Errorf("dl[0] = %+v, want projected running link", first)
	}
	if first.Finished || !first.Running() {
		t.Errorf("dl[0].Finished=%v Running()=%v, want false/true", first.Finished, first.Running())
	}
	if got := first.Progress(); got != 0.5 {
		t.Errorf("dl[0].Progress() = %v, want 0.5", got)
	}
	if !dl[1].Finished {
		t.Errorf("dl[1].Finished = false, want true")
	}
	if got := dl[1].Progress(); got != 1 {
		t.Errorf("dl[1].Progress() = %v, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// WaitDownloads
// ---------------------------------------------------------------------------

func TestWaitDownloads(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	m.queryLinksQueue = [][]jdownloader.DownloadLink{
		{{
			Uuid: ptr(int64(1)), Name: ptr("game.zip"), Url: ptr("https://h.example/g.zip"),
			Host: ptr("h.example"), Status: ptr("Running"),
			BytesTotal: ptr(int64(1000)), BytesLoaded: ptr(int64(500)),
			Speed: ptr(float64(100)), Finished: ptr(false), Enabled: ptr(true),
		}},
		{{
			Uuid: ptr(int64(1)), Name: ptr("game.zip"), Url: ptr("https://h.example/g.zip"),
			Host: ptr("h.example"), Status: ptr("Finished"),
			BytesTotal: ptr(int64(1000)), BytesLoaded: ptr(int64(1000)),
			Speed: ptr(float64(0)), Finished: ptr(true), Enabled: ptr(true),
		}},
	}
	c := newTestClient(t, m)

	var snapshots []Progress
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done, err := c.WaitDownloads(ctx,
		WithPollInterval(10*time.Millisecond),
		WithStallTimeout(2*time.Second),
		WithProgress(func(p Progress) { snapshots = append(snapshots, p) }),
	)
	if err != nil {
		t.Fatalf("WaitDownloads: %v", err)
	}
	if len(snapshots) < 2 {
		t.Errorf("progress callback saw %d snapshots, want >= 2", len(snapshots))
	}
	if !snapshots[0].Running() || snapshots[0].Finished {
		t.Errorf("first snapshot: running=%v finished=%v, want running, not finished",
			snapshots[0].Running(), snapshots[0].Finished)
	}
	if !snapshots[len(snapshots)-1].Finished {
		t.Errorf("last snapshot not finished: %+v", snapshots[len(snapshots)-1])
	}
	if len(done) != 1 || !done[0].Finished {
		t.Errorf("WaitDownloads returned %+v, want the finished link", done)
	}
}

// TestWaitDownloadsEmptyFirstPoll mirrors the real transition where
// LinkGrabber.Add(autostart=true) has not moved links into the Downloads
// list yet: the first poll is empty and must not be treated as done or as
// an instant stall.
func TestWaitDownloadsEmptyFirstPoll(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	m.queryLinksQueue = [][]jdownloader.DownloadLink{
		{},
		{{
			Uuid: ptr(int64(1)), Name: ptr("game.zip"), Url: ptr("https://h.example/g.zip"),
			Host: ptr("h.example"), Status: ptr("Finished"),
			BytesTotal: ptr(int64(10)), BytesLoaded: ptr(int64(10)),
			Finished: ptr(true), Enabled: ptr(true),
		}},
	}
	c := newTestClient(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done, err := c.WaitDownloads(ctx,
		WithPollInterval(5*time.Millisecond),
		WithStallTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("WaitDownloads: %v", err)
	}
	if len(done) != 1 || !done[0].Finished {
		t.Errorf("WaitDownloads returned %+v, want finished link after empty first poll", done)
	}
}

func TestWaitDownloadsStall(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	m.queryLinks = []jdownloader.DownloadLink{{
		Uuid: ptr(int64(1)), Name: ptr("captcha.zip"), Url: ptr("https://h.example/c.zip"),
		Host: ptr("h.example"), Status: ptr("Waiting for captcha"),
		BytesTotal: ptr(int64(1000)), BytesLoaded: ptr(int64(0)),
		Speed: ptr(float64(0)), Finished: ptr(false), Enabled: ptr(true),
	}}
	c := newTestClient(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.WaitDownloads(ctx,
		WithPollInterval(5*time.Millisecond),
		WithStallTimeout(50*time.Millisecond),
	)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("WaitDownloads: err = %v, want ErrStalled", err)
	}
}

func TestWaitDownloadsContextCancel(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	m.queryLinks = []jdownloader.DownloadLink{{
		Uuid: ptr(int64(1)), Name: ptr("slow.zip"), Url: ptr("https://h.example/s.zip"),
		Host: ptr("h.example"), Status: ptr("Running"),
		BytesTotal: ptr(int64(1 << 40)), BytesLoaded: ptr(int64(1)),
		Speed: ptr(float64(1)), Finished: ptr(false), Enabled: ptr(true),
	}}
	c := newTestClient(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := c.WaitDownloads(ctx, WithPollInterval(5*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitDownloads: err = %v, want context.DeadlineExceeded", err)
	}
}

func TestWaitDownloadsBadOptions(t *testing.T) {
	m := newMockMyJD(t, "test@example.com", "hunter2-secret")
	c := newTestClient(t, m)

	if _, err := c.WaitDownloads(context.Background(), WithPollInterval(0)); err == nil {
		t.Error("WaitDownloads with zero poll interval: expected error")
	}
	if _, err := c.WaitDownloads(context.Background(), WithPollInterval(1*time.Second), WithStallTimeout(-1)); err == nil {
		t.Error("WaitDownloads with negative stall timeout: expected error")
	}
}

// ---------------------------------------------------------------------------
// Batch helpers (white-box)
// ---------------------------------------------------------------------------

func TestBatchDone(t *testing.T) {
	finished := Download{Finished: true, Enabled: true}
	skipped := Download{Skipped: true, Enabled: true}
	disabled := Download{Enabled: false}
	running := Download{Enabled: true}

	cases := []struct {
		name string
		in   []Download
		want bool
	}{
		{"empty", nil, false},
		{"all finished", []Download{finished, finished}, true},
		{"mixed terminal", []Download{finished, skipped, disabled}, true},
		{"one running", []Download{finished, running}, false},
		{"all skipped", []Download{skipped}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := batchDone(tc.in); got != tc.want {
				t.Errorf("batchDone(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBatchProgress(t *testing.T) {
	p := batchProgress([]Download{
		{BytesLoaded: 100, BytesTotal: 200, Speed: 10, Enabled: true},
		{BytesLoaded: 50, BytesTotal: 100, Speed: 20, Enabled: true},
		{BytesLoaded: 0, BytesTotal: 0, Enabled: true}, // unknown size
	})
	if p.BytesLoaded != 150 || p.BytesTotal != 300 || p.Speed != 30 {
		t.Errorf("batchProgress aggregates = %+v, want loaded=150 total=300 speed=30", p)
	}
	if p.Finished {
		t.Error("batchProgress.Finished = true, want false")
	}
	if !p.Running() {
		t.Error("batchProgress.Running() = false, want true (speed > 0)")
	}
	if got := p.Fraction(); got != 0.5 {
		t.Errorf("batchProgress.Fraction() = %v, want 0.5", got)
	}
}

func TestProgressFractionUnknownTotal(t *testing.T) {
	if got := (Progress{BytesLoaded: 5}).Fraction(); got != 0 {
		t.Errorf("Fraction with unknown total = %v, want 0", got)
	}
	if got := (Download{BytesLoaded: 5}).Progress(); got != 0 {
		t.Errorf("Download.Progress with unknown total = %v, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// slog → zap bridge
// ---------------------------------------------------------------------------

type captureHandler struct {
	mu    sync.Mutex
	recs  []slog.Record
	attrs []slog.Attr
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	rec := r.Clone()
	h.recs = append(h.recs, rec)
	rec.Attrs(func(a slog.Attr) bool {
		h.attrs = append(h.attrs, a)
		return true
	})
	return nil
}
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler            { return h }

func TestNewZapFromSlog(t *testing.T) {
	h := &captureHandler{}
	sl := slog.New(h)
	z := NewZapFromSlog(sl)

	z.Infow("jd debug line", "action", "addLinks", "count", 3)
	z.Debug("nothing to see")
	z.Warnw("captcha pending", "host", "krakenfiles")

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recs) != 3 {
		t.Fatalf("slog saw %d records, want 3", len(h.recs))
	}
	if h.recs[0].Message != "jd debug line" {
		t.Errorf("record[0].Message = %q, want %q", h.recs[0].Message, "jd debug line")
	}
	if h.recs[0].Level != slog.LevelInfo {
		t.Errorf("record[0].Level = %v, want Info", h.recs[0].Level)
	}
	if h.recs[2].Level != slog.LevelWarn {
		t.Errorf("record[2].Level = %v, want Warn", h.recs[2].Level)
	}

	// attrs flattened across records: action + count + host
	got := map[string]bool{}
	for _, a := range h.attrs {
		got[a.Key] = true
	}
	for _, want := range []string{"action", "count", "host"} {
		if !got[want] {
			t.Errorf("missing slog attr %q in %v", want, h.attrs)
		}
	}
}

func TestNewZapFromSlogNil(t *testing.T) {
	z := NewZapFromSlog(nil)
	z.Info("must not panic")
}
