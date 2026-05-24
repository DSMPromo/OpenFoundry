package domain

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type fakeAgentClock struct{ now time.Time }

func (c fakeAgentClock) Now() time.Time { return c.now }

type fakeAgentStore struct {
	agents map[uuid.UUID]*models.ConnectorAgent
}

func (s fakeAgentStore) GetConnectorAgent(_ context.Context, id uuid.UUID) (*models.ConnectorAgent, error) {
	return s.agents[id], nil
}

func TestResolveAgentURLPrefersInlineURL(t *testing.T) {
	resolver := AgentRegistryResolver{}
	got, err := resolver.ResolveAgentURL(context.Background(), json.RawMessage(`{"agent_url":" https://agent.local "}`))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != "https://agent.local" {
		t.Fatalf("got %v", got)
	}
}

func TestResolveAgentURLRejectsOfflineAgent(t *testing.T) {
	id := uuid.New()
	resolver := AgentRegistryResolver{Store: fakeAgentStore{agents: map[uuid.UUID]*models.ConnectorAgent{id: {ID: id, Name: "edge", AgentURL: "https://edge", Status: "offline"}}}}
	_, err := resolver.ResolveAgentURL(context.Background(), json.RawMessage(`{"agent_id":"`+id.String()+`"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAgentURLRejectsStaleHeartbeat(t *testing.T) {
	id := uuid.New()
	now := time.Unix(1000, 0).UTC()
	last := now.Add(-3 * time.Minute)
	resolver := AgentRegistryResolver{Store: fakeAgentStore{agents: map[uuid.UUID]*models.ConnectorAgent{id: {ID: id, Name: "edge", AgentURL: "https://edge", Status: "online", LastHeartbeatAt: &last}}}, Clock: fakeAgentClock{now: now}, StaleAfter: 2 * time.Minute}
	_, err := resolver.ResolveAgentURL(context.Background(), json.RawMessage(`{"agent_id":"`+id.String()+`"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}
