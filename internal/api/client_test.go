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

	res, err := c.Deploy(context.Background(), 10, "https://ep", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.TokenSeed != "ts_x" || res.Created != 10 || res.AgentsByState["deactivated"] != 10 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestSetModel(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/account/model" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"model":"anthropic/claude-3.5-haiku"}`))
	})
	defer srv.Close()

	effective, err := c.SetModel(context.Background(), "anthropic/claude-3.5-haiku")
	if err != nil || effective != "anthropic/claude-3.5-haiku" {
		t.Fatalf("set model: %q err=%v", effective, err)
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

func TestSendMessageUsesTokenSeedHeader(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents/a-123/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Chariot-Token") != "ts_seed" {
			t.Errorf("X-Chariot-Token = %q", r.Header.Get("X-Chariot-Token"))
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"accepted","agent_id":"a-123","state":"active"}`))
	})
	defer srv.Close()

	ack, err := c.SendMessage(context.Background(), "a-123", "ts_seed", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "accepted" || ack.AgentID != "a-123" || ack.State != "active" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}

func TestListRepliesPagesWithSeedHeader(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/replies" || r.URL.Query().Get("after") != "7" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		if r.Header.Get("X-Chariot-Token") != "ts_seed" {
			t.Errorf("X-Chariot-Token = %q", r.Header.Get("X-Chariot-Token"))
		}
		w.Write([]byte(`{"replies":[{"id":8,"agent_id":"a1","message":"hi","reply_to":null,"created_at":"2026-07-02T10:00:00Z"}],"next_cursor":8}`))
	})
	defer srv.Close()

	page, err := c.ListReplies(context.Background(), "ts_seed", 7, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Replies) != 1 || page.Replies[0].Message != "hi" || page.NextCursor != 8 {
		t.Fatalf("unexpected page: %+v", page)
	}
}
