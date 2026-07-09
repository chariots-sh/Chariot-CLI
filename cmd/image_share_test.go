package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

const sharesFixture = `{"outgoing":[
	{"share_id":"sh_out","image_name":"research","grantee_email":"bob@chariot.test",
	 "alias":"research","created_at":"2026-07-09T00:00:00Z"}
],"incoming":[
	{"share_id":"sh_in","alias":"teamtool","owner_email":"alice@chariot.test",
	 "image_name":"tool","pod_size":"medium","ready":true,
	 "created_at":"2026-07-09T00:00:00Z"}
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
			"grantee_email":"bob@chariot.test","alias":"lab",
			"created_at":"2026-07-09T00:00:00Z"}`))
	})

	got := runCLI(t, "", "image", "share", "research", "--with", "bob@chariot.test", "--alias", "lab")
	if got.err != nil {
		t.Fatalf("image share: %v", got.err)
	}
	if body["image_name"] != "research" || body["grantee_email"] != "bob@chariot.test" || body["alias"] != "lab" {
		t.Errorf("request body = %v", body)
	}
	mustContain(t, got.stdout, "✓ shared research with bob@chariot.test (as lab)", "stdout")

	// --with is mandatory: without a grantee there is nothing to share to.
	if got := runCLI(t, "", "image", "share", "research"); got.err == nil {
		t.Error("image share without --with must fail")
	}
}

func TestImageSharesRendersBothDirections(t *testing.T) {
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

	// Grantee removing a received share: matched by alias.
	got = runCLI(t, "", "image", "unshare", "teamtool")
	if got.err != nil {
		t.Fatalf("unshare incoming: %v", got.err)
	}
	mustContain(t, got.stdout, "removed teamtool (shared by alice@chariot.test)", "stdout")

	if len(deleted) != 2 || deleted[0] != "/v1/images/shares/sh_out" || deleted[1] != "/v1/images/shares/sh_in" {
		t.Errorf("deleted = %v", deleted)
	}

	// A name matching nothing errors without deleting anything.
	if got := runCLI(t, "", "image", "unshare", "ghost"); got.err == nil {
		t.Error("unshare of an unknown name must fail")
	}
	if len(deleted) != 2 {
		t.Errorf("unexpected extra deletes: %v", deleted)
	}
}
