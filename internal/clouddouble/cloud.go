// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/altessa-s/rmcloud-go"
)

// The credentials the double hands out and accepts.
const (
	// ValidCode is the one-time code the double accepts at registration.
	ValidCode = "abcdefgh"
	// DeviceToken is the device token the double sells for ValidCode.
	DeviceToken = "device-token"
	// DeviceID is the device the tokens belong to.
	DeviceID = "device-1"
	// UserAgent is the User-Agent the client is pinned to.
	UserAgent = "desktop/3.28.0 (macos 14.5)"
)

// Slack bounds how late a socket or a timer may be observed on a loaded
// machine.
const Slack = 5 * time.Second

// Fault sees every request first; returning true means it answered and the
// double does nothing more.
type Fault func(w http.ResponseWriter, r *http.Request) bool

// Cloud is the double.
type Cloud struct {
	srv *httptest.Server

	mu         sync.Mutex
	blobs      map[string][]byte
	rootHash   string
	generation int64
	userTokens map[string]bool
	nextUser   string
	sockets    map[*websocket.Conn]struct{}
	hits       map[string]int
	fault      Fault
	revoked    int

	accepted chan struct{}
}

// New starts the double. Its first root is an empty root index at
// generation 1, so a root read succeeds from the start.
func New(tb testing.TB) *Cloud {
	tb.Helper()
	c := &Cloud{
		blobs:      map[string][]byte{},
		userTokens: map[string]bool{},
		sockets:    map[*websocket.Conn]struct{}{},
		hits:       map[string]int{},
		accepted:   make(chan struct{}, 16),
	}
	c.rootHash = c.put([]byte(rmcloud.SerializeRoot(nil)))
	c.generation = 1
	c.nextUser = UserToken(tb, "user-1")
	c.srv = httptest.NewTLSServer(c)
	tb.Cleanup(func() {
		c.mu.Lock()
		for conn := range c.sockets {
			_ = conn.CloseNow()
		}
		c.mu.Unlock()
		c.srv.Close()
	})
	return c
}

// URL is the double's base URL, both auth and sync host.
func (c *Cloud) URL() url.URL {
	u, err := url.Parse(c.srv.URL)
	if err != nil {
		panic(err)
	}
	return *u
}

// Options pin an rmcloud client to the double: its hosts, its certificate,
// a fixed User-Agent and a silent logger.
func (c *Cloud) Options() []rmcloud.Option {
	u := c.URL()
	roots := x509.NewCertPool()
	roots.AddCert(c.srv.Certificate())
	return []rmcloud.Option{
		rmcloud.WithEndpoints(rmcloud.Endpoints{AuthHost: u, SyncHost: u}),
		rmcloud.WithRootCAs(roots),
		rmcloud.WithUserAgent(UserAgent),
		rmcloud.WithLogger(slog.New(slog.DiscardHandler)),
	}
}

// Client is an rmcloud client pinned to the double over store, closed when
// the test ends.
func (c *Cloud) Client(tb testing.TB, store rmcloud.SecretStore, opts ...rmcloud.Option) *rmcloud.Client {
	tb.Helper()
	cl, err := rmcloud.New(store, append(c.Options(), opts...)...)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = cl.Close() })
	return cl
}

// Registered is a store holding a device token and a user token the double
// accepts, so a test can start at the sync calls.
func (c *Cloud) Registered(tb testing.TB) *Store {
	tb.Helper()
	token := UserToken(tb, "user-0")
	c.mu.Lock()
	c.userTokens[token] = true
	c.mu.Unlock()
	store := NewStore()
	store.m[rmcloud.SecretDeviceToken] = DeviceToken
	store.m[rmcloud.SecretDeviceID] = DeviceID
	store.m[rmcloud.SecretUserToken] = token
	return store
}

// SetFault installs f; nil removes it.
func (c *Cloud) SetFault(f Fault) {
	c.mu.Lock()
	c.fault = f
	c.mu.Unlock()
}

// Hits counts the requests to key: the method and path, with every blob
// folded into "GET /sync/v3/files/*" or "PUT /sync/v3/files/*".
func (c *Cloud) Hits(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[key]
}

// AllHits is a copy of every request count by key, for a test that counts
// what one command cost.
func (c *Cloud) AllHits() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.hits)
}

// Revoked counts the device-delete calls.
func (c *Cloud) Revoked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revoked
}

// Root is the current root hash and generation.
func (c *Cloud) Root() (string, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rootHash, c.generation
}

// Advance moves the generation as another device's write would, so the
// next write based on the old generation meets a conflict.
func (c *Cloud) Advance() {
	c.mu.Lock()
	c.generation++
	c.mu.Unlock()
}

// Blob returns a stored blob.
func (c *Cloud) Blob(hash string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.blobs[hash]
	return append([]byte(nil), b...), ok
}

// BlobCount is the number of stored blobs.
func (c *Cloud) BlobCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.blobs)
}

// put stores body and returns its address. The caller holds mu, or owns c.
func (c *Cloud) put(body []byte) string {
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	c.blobs[hash] = append([]byte(nil), body...)
	return hash
}

func key(r *http.Request) string {
	path := r.URL.Path
	if strings.HasPrefix(path, "/sync/v3/files/") {
		path = "/sync/v3/files/*"
	}
	return r.Method + " " + path
}

// ServeHTTP is the double's protocol.
func (c *Cloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.hits[key(r)]++
	fault := c.fault
	c.mu.Unlock()
	if fault != nil && fault(w, r) {
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/token/json/2/device/new":
		var body struct {
			Code string `json:"code"`
		}
		if err := json.UnmarshalRead(r.Body, &body); err != nil || body.Code != ValidCode {
			http.Error(w, "bad code", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, DeviceToken)
	case r.Method == http.MethodPost && r.URL.Path == "/token/json/2/user/new":
		if r.Header.Get("Authorization") != "Bearer "+DeviceToken {
			http.Error(w, "unknown device", http.StatusUnauthorized)
			return
		}
		c.mu.Lock()
		token := c.nextUser
		c.userTokens[token] = true
		c.mu.Unlock()
		_, _ = io.WriteString(w, token)
	case r.Method == http.MethodPost && r.URL.Path == "/token/json/3/device/delete":
		c.mu.Lock()
		c.revoked++
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == "/notifications/ws/json/1":
		if !c.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c.acceptSocket(w, r)
	case strings.HasPrefix(r.URL.Path, "/sync/"):
		if !c.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c.serveSync(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (c *Cloud) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	c.mu.Lock()
	defer c.mu.Unlock()
	return ok && c.userTokens[token]
}

func (c *Cloud) serveSync(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/sync/v4/root":
		hash, gen := c.Root()
		_, _ = fmt.Fprintf(w, `{"hash":%q,"generation":%d}`, hash, gen)
	case r.Method == http.MethodPut && r.URL.Path == "/sync/v3/root":
		c.writeRoot(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/sync/v3/check-files":
		var body struct {
			Files []string `json:"files"`
		}
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		missing := []string{}
		c.mu.Lock()
		for _, h := range body.Files {
			if _, ok := c.blobs[h]; !ok {
				missing = append(missing, h)
			}
		}
		c.mu.Unlock()
		_ = json.MarshalWrite(w, map[string][]string{"missingFiles": missing})
	case strings.HasPrefix(r.URL.Path, "/sync/v3/files/"):
		c.serveBlob(w, r, strings.TrimPrefix(r.URL.Path, "/sync/v3/files/"))
	default:
		http.NotFound(w, r)
	}
}

func (c *Cloud) serveBlob(w http.ResponseWriter, r *http.Request, hash string) {
	switch r.Method {
	case http.MethodGet:
		body, ok := c.Blob(hash)
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Header.Get("x-goog-hash") != rmcloud.GoogHash(body) {
			http.Error(w, "crc32c mismatch", http.StatusBadRequest)
			return
		}
		name := r.Header.Get("rm-filename")
		if strings.HasSuffix(name, ".docSchema") && !validIndexName(name) {
			http.Error(w, "invalid doc schema name", http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		got := c.put(body)
		c.mu.Unlock()
		if got != hash {
			http.Error(w, "address mismatch", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

// validIndexName accepts root.docSchema and <id>.docSchema with a plain id.
func validIndexName(name string) bool {
	id := strings.TrimSuffix(name, ".docSchema")
	return id != "" && !strings.ContainsAny(id, "/.")
}

func (c *Cloud) writeRoot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Broadcast  bool   `json:"broadcast"`
		Generation int64  `json:"generation"`
		Hash       string `json:"hash"`
	}
	if err := json.UnmarshalRead(r.Body, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	if body.Generation != c.generation {
		c.mu.Unlock()
		http.Error(w, "generation moved", http.StatusPreconditionFailed)
		return
	}
	if _, ok := c.blobs[body.Hash]; !ok {
		c.mu.Unlock()
		http.Error(w, "root index is not stored", http.StatusBadRequest)
		return
	}
	c.rootHash = body.Hash
	c.generation++
	gen := c.generation
	conns := make([]*websocket.Conn, 0, len(c.sockets))
	for conn := range c.sockets {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	if body.Broadcast {
		for _, conn := range conns {
			_ = conn.Write(context.WithoutCancel(r.Context()), websocket.MessageText, []byte(SyncComplete("", "")))
		}
	}
	_, _ = fmt.Fprintf(w, `{"hash":%q,"generation":%d}`, body.Hash, gen)
}

// acceptSocket upgrades and keeps reading, so the double answers pings and
// notices the client closing.
func (c *Cloud) acceptSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.sockets[conn] = struct{}{}
	c.mu.Unlock()
	select {
	case c.accepted <- struct{}{}:
	default:
	}
	ctx := context.WithoutCancel(r.Context())
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			break
		}
	}
	c.mu.Lock()
	delete(c.sockets, conn)
	c.mu.Unlock()
	_ = conn.CloseNow()
}

// WaitSocket waits until the double has registered a notification socket.
func (c *Cloud) WaitSocket(tb testing.TB) {
	tb.Helper()
	select {
	case <-c.accepted:
	case <-time.After(Slack):
		tb.Fatal("the double never registered the notification socket")
	}
}

// Broadcast sends frame to every notification socket.
func (c *Cloud) Broadcast(frame string) int {
	c.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(c.sockets))
	for conn := range c.sockets {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(frame))
	}
	return len(conns)
}

// DropSockets closes every notification socket from the double's side.
func (c *Cloud) DropSockets() int {
	c.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(c.sockets))
	for conn := range c.sockets {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close(websocket.StatusGoingAway, "maintenance")
	}
	return len(conns)
}

// SyncComplete is the frame the cloud sends when a library changed.
func SyncComplete(id, name string) string {
	return `{"message":{"attributes":{"event":"SyncComplete","sourceDeviceID":"` + DeviceID +
		`","id":"` + id + `","vissibleName":"` + name + `"}},"subscription":"s"}`
}

// tokenExpiry is when every token the double sells expires: far enough
// ahead that no test outlives it, and fixed, so the double reads no clock.
var tokenExpiry = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test value

// UserToken is an unsigned user token that expires in 2100. nonce keeps two
// tokens apart.
func UserToken(tb testing.TB, nonce string) string {
	tb.Helper()
	claims, err := json.Marshal(map[string]any{
		"exp":       tokenExpiry.Unix(),
		"level":     "connect",
		"device-id": DeviceID,
		"scopes":    "sync:full",
		"tectonic":  "eu",
		"nonce":     nonce,
	})
	if err != nil {
		tb.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(claims) + "." + enc([]byte("sig"))
}

// Store is a secret store in memory.
type Store struct {
	mu sync.Mutex
	m  map[rmcloud.SecretName]string
}

var _ rmcloud.SecretStore = (*Store)(nil)

// NewStore returns an empty store.
func NewStore() *Store { return &Store{m: map[rmcloud.SecretName]string{}} }

// Secret returns the value under which.
func (s *Store) Secret(_ context.Context, which rmcloud.SecretName) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[which]
	return v, ok, nil
}

// Save stores value under which.
func (s *Store) Save(_ context.Context, which rmcloud.SecretName, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[which] = value
	return nil
}

// Forget removes which.
func (s *Store) Forget(_ context.Context, which rmcloud.SecretName) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, which)
	return nil
}

// Has reports whether which is stored.
func (s *Store) Has(which rmcloud.SecretName) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[which]
	return ok
}
