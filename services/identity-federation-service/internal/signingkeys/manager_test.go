package signingkeys_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/signingkeys"
)

func newSealer(t *testing.T) *signingkeys.Sealer {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	s, err := signingkeys.NewSealer(key)
	require.NoError(t, err)
	return s
}

// mockClock is a controllable Clock for the rotation tests.
type mockClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func sampleClaims() *authmw.Claims {
	now := time.Now().UTC()
	return &authmw.Claims{
		Sub:   uuid.New(),
		IAT:   now.Unix(),
		EXP:   now.Add(time.Hour).Unix(),
		JTI:   uuid.New(),
		Email: "alice@example.com",
		Name:  "Alice",
		Roles: []string{"viewer"},
	}
}

// Bootstrap mints a fresh active key when the store is empty.
func TestManager_EnsureBootstrap_MintsActive(t *testing.T) {
	t.Parallel()
	store := signingkeys.NewInMemoryStore()
	mgr := signingkeys.NewManager(store, newSealer(t), signingkeys.DefaultPolicy(), nil)

	require.NoError(t, mgr.EnsureBootstrap(context.Background()))

	mat, err := mgr.ActiveKey(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, mat.Record.Kid)
	require.Equal(t, signingkeys.StatusActive, mat.Record.Status)
}

// EnsureBootstrap is idempotent — calling it twice on a healthy store
// must not mint a second key.
func TestManager_EnsureBootstrap_Idempotent(t *testing.T) {
	t.Parallel()
	store := signingkeys.NewInMemoryStore()
	mgr := signingkeys.NewManager(store, newSealer(t), signingkeys.DefaultPolicy(), nil)
	require.NoError(t, mgr.EnsureBootstrap(context.Background()))
	first, err := mgr.ActiveKey(context.Background())
	require.NoError(t, err)

	require.NoError(t, mgr.EnsureBootstrap(context.Background()))
	second, err := mgr.ActiveKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, first.Record.Kid, second.Record.Kid)
}

// Bootstrap rotates when the active key is about to expire below the
// refresh floor (24h).
func TestManager_EnsureBootstrap_RefreshesNearExpiry(t *testing.T) {
	t.Parallel()
	clk := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := signingkeys.NewInMemoryStore()
	policy := signingkeys.Policy{
		ActiveLifetime:        30 * 24 * time.Hour,
		GraceWindow:           6 * time.Hour,
		BootstrapRefreshFloor: 24 * time.Hour,
	}
	mgr := signingkeys.NewManager(store, newSealer(t), policy, clk.Now)
	require.NoError(t, mgr.EnsureBootstrap(context.Background()))
	first, err := mgr.ActiveKey(context.Background())
	require.NoError(t, err)

	// Jump to 5 minutes before the refresh floor — should rotate.
	clk.Advance(policy.ActiveLifetime - policy.BootstrapRefreshFloor + 5*time.Minute)
	require.NoError(t, mgr.EnsureBootstrap(context.Background()))
	second, err := mgr.ActiveKey(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, first.Record.Kid, second.Record.Kid, "expected refresh near expiry")
}

// Rotate 3 times. After each rotation, tokens minted with prior kids
// must still verify during the grace window and must fail after the
// grace window closes.
func TestManager_RotateThreeTimes_GraceAndExpiry(t *testing.T) {
	t.Parallel()
	clk := &mockClock{now: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)}
	store := signingkeys.NewInMemoryStore()
	policy := signingkeys.Policy{
		ActiveLifetime:        30 * 24 * time.Hour,
		GraceWindow:           6 * time.Hour,
		BootstrapRefreshFloor: 24 * time.Hour,
	}
	mgr := signingkeys.NewManager(store, newSealer(t), policy, clk.Now)
	ctx := context.Background()

	require.NoError(t, mgr.EnsureBootstrap(ctx))

	// Track the kid → token map across rotations.
	tokensByKid := map[string]string{}

	mintToken := func() (string, string) {
		t.Helper()
		mat, err := mgr.ActiveKey(ctx)
		require.NoError(t, err)
		claims := sampleClaims()
		// Push exp well past real wall-clock time: DecodeTokenRS256Multi
		// validates exp against time.Now(), not the manager's mock clock.
		claims.EXP = time.Now().Add(72 * time.Hour).Unix()
		tok, err := mgr.IssueRS256(ctx, claims)
		require.NoError(t, err)
		return mat.Record.Kid, tok
	}

	kid0, tok0 := mintToken()
	tokensByKid[kid0] = tok0

	for i := 0; i < 3; i++ {
		out, err := mgr.Rotate(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, out.ActiveKid)
		require.Equal(t, kid0, out.PreviousKid)
		// After each rotation, the previous kid + the new kid must
		// both verify.
		for kid, tok := range tokensByKid {
			claims, err := mgr.VerifyRS256(ctx, tok, "", "")
			require.NoErrorf(t, err, "rotation %d: kid %s rejected during grace", i+1, kid)
			require.NotNil(t, claims)
		}
		// Mint a token with the new active kid and keep walking
		// forward in time. Force the previous kid to expire its
		// grace window before the next loop iteration.
		newKid, newTok := mintToken()
		// Push past the 6h grace so the now-retiring kid drops out.
		clk.Advance(policy.GraceWindow + time.Minute)
		// Previous kid must now be rejected — it is past its
		// not_after and gets marked retired on the next read.
		for kid, tok := range tokensByKid {
			_, err := mgr.VerifyRS256(ctx, tok, "", "")
			require.Errorf(t, err, "rotation %d: kid %s should be rejected after grace", i+1, kid)
		}
		// Drop the expired tokens; remember the new one for the
		// next rotation cycle.
		tokensByKid = map[string]string{newKid: newTok}
		kid0 = newKid
	}
}

// JWKS publication must serialise active + retiring keys in standard
// JWK shape (kty, use, alg, kid, n, e).
func TestManager_Jwks_Shape(t *testing.T) {
	t.Parallel()
	clk := &mockClock{now: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)}
	store := signingkeys.NewInMemoryStore()
	mgr := signingkeys.NewManager(store, newSealer(t), signingkeys.DefaultPolicy(), clk.Now)
	require.NoError(t, mgr.EnsureBootstrap(context.Background()))

	jwks, err := mgr.Jwks(context.Background())
	require.NoError(t, err)
	require.Len(t, jwks.Keys, 1)
	first := jwks.Keys[0]
	require.Equal(t, "RSA", first.Kty)
	require.Equal(t, "sig", first.Use)
	require.Equal(t, "RS256", first.Alg)
	require.NotEmpty(t, first.Kid)
	require.NotEmpty(t, first.N)
	require.NotEmpty(t, first.E)

	// After a rotation, both active and retiring appear.
	_, err = mgr.Rotate(context.Background())
	require.NoError(t, err)
	jwks2, err := mgr.Jwks(context.Background())
	require.NoError(t, err)
	require.Len(t, jwks2.Keys, 2)
}

// HTTP integration: the handler returns valid JSON with ≥1 key and a
// forced rotation flips the active kid.
func TestHandler_JwksAndRotate(t *testing.T) {
	t.Parallel()
	store := signingkeys.NewInMemoryStore()
	mgr := signingkeys.NewManager(store, newSealer(t), signingkeys.DefaultPolicy(), nil)
	require.NoError(t, mgr.EnsureBootstrap(context.Background()))
	h := signingkeys.NewHandler(mgr)

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", h.Jwks)
	mux.HandleFunc("/api/v1/admin/jwks/rotate", h.Rotate)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// 1. Fetch JWKS — must have ≥1 key.
	resp, err := http.Get(srv.URL + "/.well-known/jwks.json")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body signingkeys.Jwks
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.GreaterOrEqual(t, len(body.Keys), 1)
	firstKid := body.Keys[0].Kid

	// 2. Force a rotation — admin route.
	resp2, err := http.Post(srv.URL+"/api/v1/admin/jwks/rotate", "application/json", nil)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	var outcome signingkeys.RotationOutcome
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&outcome))
	require.NotEmpty(t, outcome.ActiveKid)
	require.NotEqual(t, firstKid, outcome.ActiveKid)

	// 3. Re-fetch — JWKS must now show the new active first +
	//    the old kid retiring.
	resp3, err := http.Get(srv.URL + "/.well-known/jwks.json")
	require.NoError(t, err)
	defer resp3.Body.Close()
	var body3 signingkeys.Jwks
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&body3))
	require.Equal(t, outcome.ActiveKid, body3.Keys[0].Kid)
	require.Len(t, body3.Keys, 2)
	require.Equal(t, firstKid, body3.Keys[1].Kid)
}

// Sealer round-trips: Seal then Open returns the original bytes; a
// flipped byte fails the GCM tag.
func TestSealer_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newSealer(t)
	pt := []byte("the quick brown fox jumps over the lazy dog")
	ct, err := s.Seal(pt)
	require.NoError(t, err)
	require.NotEqual(t, pt, ct)
	back, err := s.Open(ct)
	require.NoError(t, err)
	require.Equal(t, pt, back)

	tampered := append([]byte{}, ct...)
	tampered[len(tampered)-1] ^= 0xff
	_, err = s.Open(tampered)
	require.Error(t, err)
}

// VerifierKey rejects retired kids.
func TestManager_VerifierKey_RejectsRetired(t *testing.T) {
	t.Parallel()
	clk := &mockClock{now: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)}
	store := signingkeys.NewInMemoryStore()
	policy := signingkeys.Policy{
		ActiveLifetime:        30 * 24 * time.Hour,
		GraceWindow:           1 * time.Hour,
		BootstrapRefreshFloor: 24 * time.Hour,
	}
	mgr := signingkeys.NewManager(store, newSealer(t), policy, clk.Now)
	ctx := context.Background()

	require.NoError(t, mgr.EnsureBootstrap(ctx))
	firstActive, err := mgr.ActiveKey(ctx)
	require.NoError(t, err)

	_, err = mgr.Rotate(ctx)
	require.NoError(t, err)

	// Within grace — old kid still resolves.
	_, err = mgr.VerifierKey(ctx, firstActive.Record.Kid)
	require.NoError(t, err)

	clk.Advance(policy.GraceWindow + time.Minute)

	// Past grace — old kid is retired.
	_, err = mgr.VerifierKey(ctx, firstActive.Record.Kid)
	require.ErrorIs(t, err, signingkeys.ErrNoActiveKey)
}
