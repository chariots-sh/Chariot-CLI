package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestDeployFleetPackBodyAndResult(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/fleets/deploy" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "quant-firm" || body["owner_email"] != "max@example.com" {
			t.Errorf("unexpected body: %+v", body)
		}
		// Unset optionals are omitted, not sent as "".
		for _, key := range []string{"endpoint", "model"} {
			if _, present := body[key]; present {
				t.Errorf("body should omit %q", key)
			}
		}
		_, _ = w.Write([]byte(`{"token_seed":"ts_x","namespace":"cust-1","created":2,"total":2,
			"agents_by_state":{"deactivated":2},"model":"m","pack_name":"quant-firm",
			"owner_email":"max@example.com",
			"groups":[{"image_name":"brain","deploy_name":"max-brain","count":2,
			           "slugs":["agent-000000","agent-000001"],"pod_size":"medium"}],
			"skill_content":"# setup"}`))
	})
	defer srv.Close()

	res, err := c.DeployFleetPack(context.Background(), "quant-firm", "max@example.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.TokenSeed != "ts_x" || len(res.Groups) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Groups[0].DeployName != "max-brain" || len(res.Groups[0].Slugs) != 2 {
		t.Errorf("unexpected group: %+v", res.Groups[0])
	}
	if res.SkillContent == nil || *res.SkillContent != "# setup" {
		t.Errorf("unexpected skill: %v", res.SkillContent)
	}
}

func TestGetPublicFleetPackEncodesQuery(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/fleets/public/pack" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("owner_email") != "max+quant@example.com" {
			t.Errorf("owner_email not round-tripped: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"owner_email":"max+quant@example.com","name":"quant-firm",
			"description":null,"items":[],"total_agents":0,"total_daily_fee_dollars":0,
			"has_skill":false,"published_at":"2026-07-10T00:00:00Z","deploy_count":0}`))
	})
	defer srv.Close()

	pack, err := c.GetPublicFleetPack(context.Background(), "max+quant@example.com", "quant-firm")
	if err != nil {
		t.Fatal(err)
	}
	if pack.OwnerEmail != "max+quant@example.com" {
		t.Errorf("unexpected pack: %+v", pack)
	}
}
