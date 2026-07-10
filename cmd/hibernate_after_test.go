package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

// `hibernate-after set` parses dd:hh:mm into seconds for the backend; `set
// default` must send a JSON null so the backend restores its 48h default.
func TestHibernateAfterSetSendsSecondsAndNullReset(t *testing.T) {
	var bodies []map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/account/hibernate-after" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"hibernate_after_seconds":14400}`))
	})

	got := runCLI(t, "", "hibernate-after", "set", "00:04:00")
	if got.err != nil {
		t.Fatalf("hibernate-after set: %v", got.err)
	}
	if bodies[0]["seconds"].(float64) != 4*3600 {
		t.Errorf("seconds = %v", bodies[0]["seconds"])
	}
	mustContain(t, got.stdout, "✓ agents hibernate after 00:04:00 idle", "stdout")

	if got := runCLI(t, "", "hibernate-after", "set", "default"); got.err != nil {
		t.Fatalf("hibernate-after set default: %v", got.err)
	}
	if raw, present := bodies[1]["seconds"]; !present || raw != nil {
		t.Errorf("reset must send JSON null, got %v (present=%v)", bodies[1]["seconds"], present)
	}

	// A malformed duration fails locally, before any request.
	if got := runCLI(t, "", "hibernate-after", "set", "1h30m"); got.err == nil {
		t.Fatal("want a parse error for a non-dd:hh:mm duration")
	}
}

// `hibernate-after set --agent <id>` overrides one agent via
// PUT /v1/agents/{id}/hibernate-after, NOT the account-wide endpoint, and
// still parses dd:hh:mm into seconds.
func TestHibernateAfterSetAgentTargetsOneAgent(t *testing.T) {
	var body map[string]any
	var path string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"hibernate_after_seconds":21600}`))
	})

	got := runCLI(t, "", "hibernate-after", "set", "00:06:00", "--agent", "agent-123")
	if got.err != nil {
		t.Fatalf("hibernate-after set --agent: %v", got.err)
	}
	if path != "/v1/agents/agent-123/hibernate-after" {
		t.Errorf("path = %q, want per-agent endpoint", path)
	}
	if body["seconds"].(float64) != 6*3600 {
		t.Errorf("seconds = %v", body["seconds"])
	}
	mustContain(t, got.stdout, "✓ agent agent-123 hibernates after 00:06:00 idle", "stdout")
}

func TestParseDDHHMM(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"02:00:00", 2 * 86400},  // 2 days
		{"00:01:00", 3600},       // 1 hour
		{"00:00:10", 600},        // 10 minutes
		{"01:30", 90 * 60},       // hh:mm form
		{"45", 45 * 60},          // plain minutes
		{"00:48:00", 48 * 3600},  // overflowed hours mean what they say
		{"90:00:00", 90 * 86400}, // 90 days
	}
	for _, c := range cases {
		got, err := parseDDHHMM(c.in)
		if err != nil || got != c.want {
			t.Errorf("parseDDHHMM(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}

	for _, bad := range []string{"", "abc", "1:2:3:4", "-1:00:00", "1h30m", "1: :3"} {
		if _, err := parseDDHHMM(bad); err == nil {
			t.Errorf("parseDDHHMM(%q) should fail", bad)
		}
	}
}

func TestFormatDDHHMM(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{2 * 86400, "02:00:00"},
		{3600, "00:01:00"},
		{600, "00:00:10"},
		{90 * 86400, "90:00:00"},
		{0, "00:00:00"},
	}
	for _, c := range cases {
		if got := formatDDHHMM(c.in); got != c.want {
			t.Errorf("formatDDHHMM(%d) = %q; want %q", c.in, got, c.want)
		}
	}
}
