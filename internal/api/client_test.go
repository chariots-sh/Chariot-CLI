package api

import (
	"context"
	"encoding/json"
	"errors"
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
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding deploy body: %v", err)
		}
		if body["image"] != "zeroclaw" {
			t.Errorf("deploy body missing image: %v", body)
		}
		w.Write([]byte(`{"token_seed":"ts_x","namespace":"cust-1","created":10,"total":10,"agents_by_state":{"deactivated":10},"image":"zeroclaw"}`))
	})
	defer srv.Close()

	res, err := c.Deploy(context.Background(), 10, "https://ep", "", "zeroclaw")
	if err != nil {
		t.Fatal(err)
	}
	if res.TokenSeed != "ts_x" || res.Created != 10 || res.AgentsByState["deactivated"] != 10 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Image != "zeroclaw" {
		t.Fatalf("image not parsed: %+v", res)
	}
}

func TestSetHibernateAfterSendsSecondsAndNullReset(t *testing.T) {
	var sent []map[string]any
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/account/hibernate-after" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = append(sent, body)
		w.Write([]byte(`{"hibernate_after_seconds":3600}`))
	})
	defer srv.Close()

	effective, err := c.SetHibernateAfter(context.Background(), 3600)
	if err != nil || effective != 3600 {
		t.Fatalf("set hibernate-after: %d err=%v", effective, err)
	}
	if sent[0]["seconds"].(float64) != 3600 {
		t.Fatalf("unexpected body: %+v", sent[0])
	}

	// seconds <= 0 resets via an explicit null.
	if _, err := c.SetHibernateAfter(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if v, present := sent[1]["seconds"]; !present || v != nil {
		t.Fatalf("reset should send null seconds: %+v", sent[1])
	}
}

func TestSetDefaultImage(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/account/default-image" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["image"] != "research" {
			t.Errorf("unexpected body: %+v", body)
		}
		w.Write([]byte(`{"default_image":"research"}`))
	})
	defer srv.Close()

	effective, err := c.SetDefaultImage(context.Background(), "research")
	if err != nil || effective != "research" {
		t.Fatalf("set default image: %q err=%v", effective, err)
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
			w.Write([]byte(`{"agents":[{"id":"a1","slug":"agent-0","state":"active","image":"zeroclaw"}],"next_cursor":"c2"}`))
			return
		}
		w.Write([]byte(`{"agents":[{"id":"a2","slug":"agent-1","state":"deactivated","image":null}],"next_cursor":""}`))
	})
	defer srv.Close()

	p1, err := c.ListAgents(context.Background(), "", 50)
	if err != nil || len(p1.Agents) != 1 || p1.NextCursor != "c2" {
		t.Fatalf("page1: %+v err=%v", p1, err)
	}
	if p1.Agents[0].Image == nil || *p1.Agents[0].Image != "zeroclaw" {
		t.Fatalf("page1 image not parsed: %+v", p1.Agents[0])
	}
	p2, err := c.ListAgents(context.Background(), p1.NextCursor, 50)
	if err != nil || p2.Agents[0].ID != "a2" || p2.NextCursor != "" {
		t.Fatalf("page2: %+v err=%v", p2, err)
	}
	if p2.Agents[0].Image != nil {
		t.Fatalf("null image should stay nil: %+v", p2.Agents[0])
	}
}

func TestHibernateAgent(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/agents/my-agent-3/hibernate" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"id":"a1","slug":"my-agent-3","state":"hibernating","image":null}`))
	})
	defer srv.Close()

	agent, err := c.HibernateAgent(context.Background(), "my-agent-3")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Slug != "my-agent-3" || agent.State != "hibernating" {
		t.Fatalf("unexpected agent: %+v", agent)
	}
}

func TestHibernateAgentSurfacesNotFound(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"agent not found"}`))
	})
	defer srv.Close()

	_, err := c.HibernateAgent(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 404 || apiErr.Detail != "agent not found" {
		t.Fatalf("unexpected error: %v", err)
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

func TestAPIErrorMessage(t *testing.T) {
	withDetail := &APIError{Status: 402, Detail: "out of credits"}
	if got, want := withDetail.Error(), "out of credits (HTTP 402)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// A bodyless error still names its status.
	bare := &APIError{Status: 500}
	if got, want := bare.Error(), "HTTP 500"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestStartDeviceAuthParsesChallenge(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/device/start" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"device_code":"dc_1","user_code":"WXYZ-1234",
			"verification_uri_complete":"https://app.chariots.sh/device?c=WXYZ-1234",
			"interval":5,"expires_in":600}`))
	})
	defer srv.Close()

	got, err := c.StartDeviceAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceCode != "dc_1" || got.UserCode != "WXYZ-1234" || got.Interval != 5 || got.ExpiresIn != 600 {
		t.Fatalf("unexpected challenge: %+v", got)
	}
}

// A denied device-auth poll must surface as an error, not as an empty token
// (which the caller reads as "still pending" and would spin on forever).
func TestPollDeviceAuthSurfacesDenial(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":"access_denied"}`))
	})
	defer srv.Close()

	tok, err := c.PollDeviceAuth(context.Background(), "dc_1")
	if err == nil {
		t.Fatal("want an error on denial")
	}
	if tok != "" {
		t.Errorf("token = %q, want empty", tok)
	}
}

func TestDeleteAgentIssuesDelete(t *testing.T) {
	var method, path string
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	if err := c.DeleteAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || path != "/v1/agents/agent-1" {
		t.Errorf("unexpected request: %s %s", method, path)
	}
}

func TestDeleteAgentSurfacesNotFound(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"no such agent"}`))
	})
	defer srv.Close()

	err := c.DeleteAgent(context.Background(), "ghost")
	if err == nil {
		t.Fatal("want an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("want a 404 APIError, got %v", err)
	}
}

func TestAccountParsesCreditsAndAgents(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"email":"a@b.c","status":"active","credit_dollars":12.5,
			"token_prefixes":["ts_abc"],"agents_by_state":{"active":3,"deactivated":7},"model":"m"}`))
	})
	defer srv.Close()

	got, err := c.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "a@b.c" || got.CreditDollars != 12.5 || got.Model != "m" {
		t.Fatalf("unexpected account: %+v", got)
	}
	if got.AgentsByState["active"] != 3 || got.AgentsByState["deactivated"] != 7 {
		t.Errorf("agents_by_state = %v", got.AgentsByState)
	}
}
