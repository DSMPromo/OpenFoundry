package httpruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func TestClientGetReturnsEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
		require.Equal(t, "custom-value", r.Header.Get("X-Custom"))
		w.Header().Set("X-Echo", "yes")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(srv.Client(), nil, true)
	headers := http.Header{"X-Custom": []string{"custom-value"}}
	env, err := c.Get(context.Background(), nil, parseURL(t, srv.URL+"/probe"), headers, "abc", "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, env.Status)
	require.JSONEq(t, `{"ok":true}`, string(env.Bytes))
	require.Equal(t, "yes", env.Headers["x-echo"])
}

func TestClientGetEnforcesEgressPolicy(t *testing.T) {
	c := New(http.DefaultClient, []string{"api.example.com"}, false)
	_, err := c.Get(context.Background(), nil, parseURL(t, "https://other.example.net/v1"), nil, "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "egress policy does not allow")
}

func TestClientGetReturnsNon2xxAsEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
	}))
	defer srv.Close()

	c := New(srv.Client(), nil, true)
	env, err := c.Get(context.Background(), nil, parseURL(t, srv.URL), nil, "", "")
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, env.Status)
	require.Equal(t, []byte("unavailable"), env.Bytes)
}

func TestClientPostJSONSendsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		require.JSONEq(t, `{"q":"select 1"}`, string(body))
		_, _ = w.Write([]byte(`{"rows":[1]}`))
	}))
	defer srv.Close()

	c := New(srv.Client(), nil, true)
	env, err := c.PostJSON(context.Background(), nil,
		parseURL(t, srv.URL+"/query"), nil, "",
		map[string]any{"q": "select 1"}, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, env.Status)
	require.JSONEq(t, `{"rows":[1]}`, string(env.Bytes))
}

func TestClientPostFormUrlEncodesValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		require.Equal(t, "abc", r.Form.Get("client_id"))
		_, _ = w.Write([]byte(`{"access_token":"x"}`))
	}))
	defer srv.Close()

	c := New(srv.Client(), nil, true)
	env, err := c.PostForm(context.Background(), nil,
		parseURL(t, srv.URL+"/token"), nil,
		[][2]string{{"grant_type", "client_credentials"}, {"client_id", "abc"}},
		"")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, env.Status)
}

func TestClientGetRoutesThroughAgent(t *testing.T) {
	body := []byte(`{"ok":true,"via":"agent"}`)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/connector-agent/http", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var envelope agentProxyRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&envelope))
		require.Equal(t, http.MethodGet, envelope.Method)
		require.Equal(t, "https://upstream.example.com/probe", envelope.URL)
		require.Equal(t, "abc", envelope.BearerToken)

		response := agentProxyResponse{
			Status:     http.StatusOK,
			Headers:    map[string]string{"x-via": "agent"},
			BodyBase64: base64.StdEncoding.EncodeToString(body),
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer agent.Close()

	c := New(agent.Client(), nil, true)
	c.AllowedEgressHosts = []string{"upstream.example.com"}
	env, err := c.Get(context.Background(), nil,
		parseURL(t, "https://upstream.example.com/probe"), nil, "abc", agent.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, env.Status)
	require.Equal(t, "agent", env.Headers["x-via"])
	require.Equal(t, body, env.Bytes)
}

func TestHeaderMapExtractsConfigHeaders(t *testing.T) {
	headers, err := HeaderMap(map[string]any{
		"headers": map[string]any{
			"X-Trace":  "abc-123",
			"X-Tenant": "openfoundry",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "abc-123", headers.Get("X-Trace"))
	require.Equal(t, "openfoundry", headers.Get("X-Tenant"))
}

func TestHeaderMapRejectsNonStringValues(t *testing.T) {
	_, err := HeaderMap(map[string]any{
		"headers": map[string]any{"X-Bad": 42},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")
}

func TestHeaderMapNilConfig(t *testing.T) {
	headers, err := HeaderMap(nil)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestJSONBodyValidates(t *testing.T) {
	env := &ResponseEnvelope{Bytes: []byte(`{"ok":true}`)}
	body, err := JSONBody(env)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))

	bad := &ResponseEnvelope{Bytes: []byte(`not json`)}
	_, err = JSONBody(bad)
	require.Error(t, err)
}
