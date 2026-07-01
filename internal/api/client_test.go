package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(h http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(h)
	return New(srv.URL, "tok"), srv
}

func TestPollDeviceAuthPendingThenToken(t *testing.T) {
	calls := 0
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusAccepted) // pending
			return
		}
		w.Write([]byte(`{"token":"jwt-abc"}`))
	})
	defer srv.Close()

	tok, err := c.PollDeviceAuth(context.Background(), "dev")
	if err != nil || tok != "" {
		t.Fatalf("first poll should be pending: tok=%q err=%v", tok, err)
	}
	tok, err = c.PollDeviceAuth(context.Background(), "dev")
	if err != nil || tok != "jwt-abc" {
		t.Fatalf("second poll should return token: tok=%q err=%v", tok, err)
	}
}

func TestDeployParsesResult(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer auth")
		}
		w.Write([]byte(`{"token_seed":"ts_x","namespace":"cust-1","created":10,"total":10,"agents_by_state":{"deactivated":10}}`))
	})
	defer srv.Close()

	res, err := c.Deploy(context.Background(), 10, "https://ep")
	if err != nil {
		t.Fatal(err)
	}
	if res.TokenSeed != "ts_x" || res.Created != 10 || res.AgentsByState["deactivated"] != 10 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestListAgentsPagination(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			w.Write([]byte(`{"agents":[{"id":"a1","slug":"agent-0","state":"active"}],"next_cursor":"c2"}`))
			return
		}
		w.Write([]byte(`{"agents":[{"id":"a2","slug":"agent-1","state":"deactivated"}],"next_cursor":""}`))
	})
	defer srv.Close()

	p1, err := c.ListAgents(context.Background(), "", 50)
	if err != nil || len(p1.Agents) != 1 || p1.NextCursor != "c2" {
		t.Fatalf("page1: %+v err=%v", p1, err)
	}
	p2, err := c.ListAgents(context.Background(), p1.NextCursor, 50)
	if err != nil || p2.Agents[0].ID != "a2" || p2.NextCursor != "" {
		t.Fatalf("page2: %+v err=%v", p2, err)
	}
}

func TestErrorDetailSurfaced(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"detail":"insufficient credits"}`))
	})
	defer srv.Close()

	_, err := c.Account(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 402 || apiErr.Detail != "insufficient credits" {
		t.Fatalf("unexpected error: %v", err)
	}
}
