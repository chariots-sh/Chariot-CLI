package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

const sharesFixture = `{"outgoing":[
	{"share_id":"sh_out","image_name":"research","grantee_email":"bob@chariot.test",
	 "alias":"research","status":"active","created_at":"2026-07-09T00:00:00Z"}
],"incoming":[
	{"share_id":"sh_in","alias":"teamtool","owner_email":"alice@chariot.test",
	 "image_name":"tool","pod_size":"medium","accepted_pod_size":"medium",
	 "status":"active","created_at":"2026-07-09T00:00:00Z"},
	{"share_id":"sh_pending","alias":null,"owner_email":"carol@chariot.test",
	 "image_name":"scraper","pod_size":"small","accepted_pod_size":null,
	 "status":"pending","created_at":"2026-07-09T00:00:00Z"}
]}`

func TestImageShareSendsRequest(t *testing.T) {
	var body map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/shares" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"share_id":"sh_1","image_name":"research",
			"grantee_email":"bob@chariot.test","alias":null,"status":"pending",
			"created_at":"2026-07-09T00:00:00Z"}`))
	})

	got := runCLI(t, "", "image", "share", "research", "--with", "bob@chariot.test")
	if got.err != nil {
		t.Fatalf("image share: %v", got.err)
	}
	if body["image_name"] != "research" || body["grantee_email"] != "bob@chariot.test" {
		t.Errorf("request body = %v", body)
	}
	mustContain(t, got.stdout, "✓ offered research to bob@chariot.test", "stdout")
	mustContain(t, got.stdout, "Pending their acceptance", "stdout")

	// --with is mandatory: without a grantee there is nothing to share to.
	if got := runCLI(t, "", "image", "share", "research"); got.err == nil {
		t.Error("image share without --with must fail")
	}
}

func TestImageAcceptResolvesPendingOfferAndSendsAlias(t *testing.T) {
	var acceptBodies []map[string]any
	var acceptPaths []string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/shares":
			_, _ = w.Write([]byte(sharesFixture))
		case r.Method == http.MethodPost:
			acceptPaths = append(acceptPaths, r.URL.Path)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			acceptBodies = append(acceptBodies, body)
			_, _ = w.Write([]byte(`{"share_id":"sh_pending","alias":"webscraper",
				"accepted_pod_size":"small"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	// A pending offer is matched by its image name; --alias rides along.
	got := runCLI(t, "", "image", "accept", "scraper", "--alias", "webscraper")
	if got.err != nil {
		t.Fatalf("image accept: %v", got.err)
	}
	if len(acceptPaths) != 1 || acceptPaths[0] != "/v1/images/shares/sh_pending/accept" {
		t.Errorf("accept paths = %v", acceptPaths)
	}
	if acceptBodies[0]["alias"] != "webscraper" {
		t.Errorf("accept body = %v", acceptBodies[0])
	}
	mustContain(t, got.stdout, "✓ accepted webscraper from carol@chariot.test (pod size small)", "stdout")

	// An accepted share is matched by its alias (the re-accept path).
	if got := runCLI(t, "", "image", "accept", "teamtool"); got.err != nil {
		t.Fatalf("image accept teamtool: %v", got.err)
	}
	if len(acceptPaths) != 2 || acceptPaths[1] != "/v1/images/shares/sh_in/accept" {
		t.Errorf("accept paths = %v", acceptPaths)
	}

	// An unknown name errors without POSTing.
	if got := runCLI(t, "", "image", "accept", "ghost"); got.err == nil {
		t.Error("accept of an unknown name must fail")
	}
	if len(acceptPaths) != 2 {
		t.Errorf("unexpected extra accepts: %v", acceptPaths)
	}
}

func TestImageSharesRendersBothDirectionsWithStatus(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/shares" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(sharesFixture))
	})

	got := runCLI(t, "", "image", "shares")
	if got.err != nil {
		t.Fatalf("image shares: %v", got.err)
	}
	mustContain(t, got.stdout, "SHARED BY YOU", "stdout")
	mustContain(t, got.stdout, "bob@chariot.test", "stdout")
	mustContain(t, got.stdout, "SHARED WITH YOU", "stdout")
	mustContain(t, got.stdout, "teamtool", "stdout")
	mustContain(t, got.stdout, "alice@chariot.test", "stdout")
	// The pending offer is listed under the owner's image name with a nudge.
	mustContain(t, got.stdout, "scraper", "stdout")
	mustContain(t, got.stdout, "pending — `chariot image accept`", "stdout")
}

func TestImageUnshareResolvesTheRightShare(t *testing.T) {
	var deleted []string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/shares":
			_, _ = w.Write([]byte(sharesFixture))
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	// Owner revoking a grant: matched by image name + grantee email.
	got := runCLI(t, "", "image", "unshare", "research", "--with", "bob@chariot.test")
	if got.err != nil {
		t.Fatalf("unshare --with: %v", got.err)
	}
	mustContain(t, got.stdout, "no longer shared with bob@chariot.test", "stdout")

	// Grantee removing an accepted share: matched by alias.
	got = runCLI(t, "", "image", "unshare", "teamtool")
	if got.err != nil {
		t.Fatalf("unshare incoming: %v", got.err)
	}
	mustContain(t, got.stdout, "removed teamtool (shared by alice@chariot.test)", "stdout")

	// Grantee declining a PENDING offer: matched by its image name.
	got = runCLI(t, "", "image", "unshare", "scraper")
	if got.err != nil {
		t.Fatalf("unshare pending: %v", got.err)
	}

	want := []string{"/v1/images/shares/sh_out", "/v1/images/shares/sh_in", "/v1/images/shares/sh_pending"}
	if len(deleted) != 3 || deleted[0] != want[0] || deleted[1] != want[1] || deleted[2] != want[2] {
		t.Errorf("deleted = %v", deleted)
	}

	// A name matching nothing errors without deleting anything.
	if got := runCLI(t, "", "image", "unshare", "ghost"); got.err == nil {
		t.Error("unshare of an unknown name must fail")
	}
	if len(deleted) != 3 {
		t.Errorf("unexpected extra deletes: %v", deleted)
	}
}
