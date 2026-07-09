package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestImagesRendersCatalog(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/builtin" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"images":[
			{"name":"zeroclaw","description":"Stock agent","pod_size":"small",
			 "available":true,"default":true,"daily_fee_dollars":0.05},
			{"name":"hermes","description":"Coming later","pod_size":"medium",
			 "available":false,"default":false,"daily_fee_dollars":0.2}
		],"custom_images":[
			{"name":"research","pod_size":"medium","default":false,
			 "daily_fee_dollars":0.2,"ready_at":"2026-07-03T00:00:00Z"}
		],"default_image":"zeroclaw"}`))
	})

	got := runCLI(t, "", "images")
	if got.err != nil {
		t.Fatalf("images: %v", got.err)
	}
	mustContain(t, got.stdout, "zeroclaw (default)", "stdout")
	mustContain(t, got.stdout, "available", "stdout")
	mustContain(t, got.stdout, "hermes", "stdout")
	// An unavailable image must be labelled, not silently listed as deployable.
	mustContain(t, got.stdout, "coming soon", "stdout")
	// Fees render as two-decimal dollars.
	mustContain(t, got.stdout, "$0.05", "stdout")
	mustContain(t, got.stdout, "$0.20", "stdout")
	// The account's verified custom images list alongside the builtins.
	mustContain(t, got.stdout, "research", "stdout")
	mustContain(t, got.stdout, "Your custom image.", "stdout")
}

// `images set-default` is the documented way to choose what NULL-image agents
// run; `set-default default` must send a JSON null to reset.
func TestImagesSetDefaultSendsNameAndNullReset(t *testing.T) {
	var bodies []map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/account/default-image" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"default_image":"research"}`))
	})

	got := runCLI(t, "", "images", "set-default", "research")
	if got.err != nil {
		t.Fatalf("images set-default: %v", got.err)
	}
	if bodies[0]["image"] != "research" {
		t.Errorf("image = %v", bodies[0]["image"])
	}
	mustContain(t, got.stdout, "✓ default image: research", "stdout")

	if got := runCLI(t, "", "images", "set-default", "default"); got.err != nil {
		t.Fatalf("images set-default default: %v", got.err)
	}
	if raw, present := bodies[1]["image"]; !present || raw != nil {
		t.Errorf("reset must send JSON null, got %v (present=%v)", bodies[1]["image"], present)
	}
}

func TestImageStatusRendersFailure(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/current" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"img_1","status":"failed","pod_size":"medium",
			"failed_phase":"verifying","error":"agent never replied"}`))
	})

	got := runCLI(t, "", "image", "status")
	if got.err != nil {
		t.Fatalf("image status: %v", got.err)
	}
	mustContain(t, got.stdout, "img_1", "stdout")
	mustContain(t, got.stdout, "failed", "stdout")
	mustContain(t, got.stdout, "verifying", "stdout")
	mustContain(t, got.stdout, "agent never replied", "stdout")
}

// `image guidelines` is pure text and needs no login or network.
func TestImageGuidelinesPrintsContract(t *testing.T) {
	logout(t)
	got := runCLI(t, "", "image", "guidelines")
	if got.err != nil {
		t.Fatalf("image guidelines: %v", got.err)
	}
	for _, want := range []string{
		"CHARIOT CUSTOM AGENT IMAGE",
		"/zeroclaw-data",
		":8088",
		"$CHARIOT_OUTBOUND_URL",
	} {
		mustContain(t, got.stdout, want, "stdout")
	}
}
