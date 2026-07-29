package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The point of the command: session auth (no token-seed), and the reply that
// comes back correlated to this send is the one printed.
func TestMessageSendsWithSessionAndPrintsCorrelatedReply(t *testing.T) {
	var correlation string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session-jwt" {
			t.Errorf("authorization = %q, want the session token", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Chariot-Token") != "" {
			t.Error("sent a token-seed; the session alone should authenticate")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/agents/research-bot/messages":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding send body: %v", err)
			}
			if body["message"] != "status?" {
				t.Errorf("message = %q", body["message"])
			}
			correlation = body["reply_to"]
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"accepted","agent_id":"agent-0001","state":"active"}`))
		case "GET /v1/replies":
			// The pre-send drain runs before correlation is set; afterwards
			// serve one matching reply plus an unrelated agent's traffic.
			if correlation == "" || r.URL.Query().Get("after") == "" {
				_, _ = w.Write([]byte(`{"replies":[],"next_cursor":0}`))
				return
			}
			_, _ = w.Write([]byte(`{"replies":[
				{"id":1,"agent_id":"agent-0002","message":"someone else's reply","reply_to":"other","created_at":"2026-07-28T10:00:00Z"},
				{"id":2,"agent_id":"agent-0001","message":"all good here","reply_to":"` + correlation + `","created_at":"2026-07-28T10:00:01Z"}
			],"next_cursor":2}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "message", "research-bot", "status?")
	if got.err != nil {
		t.Fatalf("message: %v", got.err)
	}
	mustContain(t, got.stdout, "all good here", "stdout")
	mustNotContain(t, got.stdout, "someone else's reply", "stdout")
}

func TestInboxPrintsStoredReplies(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/replies" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("after") == "0" {
			_, _ = w.Write([]byte(`{"replies":[{"id":7,"agent_id":"agent-0001","message":"done","reply_to":null,"created_at":"2026-07-28T10:00:00Z"}],"next_cursor":7}`))
			return
		}
		_, _ = w.Write([]byte(`{"replies":[],"next_cursor":7}`))
	})

	got := runCLI(t, "", "inbox")
	if got.err != nil {
		t.Fatalf("inbox: %v", got.err)
	}
	mustContain(t, got.stdout, "agent agent-0001", "stdout")
	mustContain(t, got.stdout, "done", "stdout")
}

func TestMessageRequiresLogin(t *testing.T) {
	logout(t)
	got := runCLI(t, "", "message", "research-bot", "hi")
	if got.err == nil {
		t.Fatal("want an error when not logged in")
	}
	mustContain(t, got.err.Error(), "not logged in", "error")
}
