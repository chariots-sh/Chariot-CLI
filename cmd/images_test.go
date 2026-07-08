package cmd

import (
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
		]}`))
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
