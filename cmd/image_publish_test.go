package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

const publicCatalogFixture = `{"listings":[
	{"image_name":"research","owner_email":"alice@chariot.test",
	 "description":"Deep-research agent","pod_size":"medium",
	 "daily_fee_dollars":2.0,"published_at":"2026-07-09T00:00:00Z"},
	{"image_name":"scraper","owner_email":"carol@chariot.test",
	 "description":null,"pod_size":"small",
	 "daily_fee_dollars":0.5,"published_at":"2026-07-09T00:00:00Z"}
],"next_cursor":null}`

func TestImagePublishAndUnpublishSendRequests(t *testing.T) {
	var published map[string]any
	var deleted []string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/listings":
			_ = json.NewDecoder(r.Body).Decode(&published)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"image_name":"research","description":"Deep-research agent",
				"published_at":"2026-07-09T00:00:00Z"}`))
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "", "image", "publish", "research", "--description", "Deep-research agent")
	if got.err != nil {
		t.Fatalf("image publish: %v", got.err)
	}
	if published["image_name"] != "research" || published["description"] != "Deep-research agent" {
		t.Errorf("publish body = %v", published)
	}
	mustContain(t, got.stdout, "✓ research is now public", "stdout")

	got = runCLI(t, "", "image", "unpublish", "research")
	if got.err != nil {
		t.Fatalf("image unpublish: %v", got.err)
	}
	if len(deleted) != 1 || deleted[0] != "/v1/images/listings/research" {
		t.Errorf("deleted = %v", deleted)
	}
	mustContain(t, got.stdout, "✓ research is no longer public", "stdout")
	// The consequence that matters: existing adds are NOT revoked.
	mustContain(t, got.stdout, "Existing adds keep working", "stdout")
}

func TestImageBrowseRendersCatalog(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/public" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(publicCatalogFixture))
	})

	got := runCLI(t, "", "image", "browse")
	if got.err != nil {
		t.Fatalf("image browse: %v", got.err)
	}
	mustContain(t, got.stdout, "research", "stdout")
	mustContain(t, got.stdout, "alice@chariot.test", "stdout")
	mustContain(t, got.stdout, "Deep-research agent", "stdout")
	mustContain(t, got.stdout, "$2.00", "stdout")
	mustContain(t, got.stdout, "scraper", "stdout")
}

func TestImageAddResolvesOwnerFromCatalog(t *testing.T) {
	var addBody map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/public":
			_, _ = w.Write([]byte(publicCatalogFixture))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/public/add":
			_ = json.NewDecoder(r.Body).Decode(&addBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"share_id":"sh_1","alias":"lab","accepted_pod_size":"medium"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "", "image", "add", "research", "--alias", "lab")
	if got.err != nil {
		t.Fatalf("image add: %v", got.err)
	}
	if addBody["owner_email"] != "alice@chariot.test" || addBody["image_name"] != "research" || addBody["alias"] != "lab" {
		t.Errorf("add body = %v", addBody)
	}
	mustContain(t, got.stdout, "✓ added lab from alice@chariot.test (pod size medium)", "stdout")

	// A name not in the catalog errors without POSTing.
	addBody = nil
	if got := runCLI(t, "", "image", "add", "ghost"); got.err == nil {
		t.Error("add of an unlisted name must fail")
	}
	if addBody != nil {
		t.Errorf("unexpected add request: %v", addBody)
	}
}
