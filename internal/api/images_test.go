package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCreateImageSendsDeclaration(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["size_bytes"].(float64) != 42 || body["sha256"] != "ab12" || body["replace"] != true {
			t.Errorf("unexpected body: %+v", body)
		}
		if body["pod_size"] != "medium" {
			t.Errorf("unexpected pod_size: %+v", body["pod_size"])
		}
		if body["name"] != "research" {
			t.Errorf("unexpected name: %+v", body["name"])
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"image_id":"img-1","chunk_size_bytes":16777216}`))
	})
	defer srv.Close()

	res, err := c.CreateImage(context.Background(), 42, "ab12", "medium", "research", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.ImageID != "img-1" || res.ChunkSizeBytes != 16777216 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestPutImageChunkRawBodyAndAck(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/img-1/chunks/16" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("unexpected content type: %s", ct)
		}
		data, _ := io.ReadAll(r.Body)
		if string(data) != "chunk-bytes" {
			t.Errorf("unexpected body: %q", data)
		}
		w.Write([]byte(`{"committed_bytes":27,"complete":false}`))
	})
	defer srv.Close()

	ack, err := c.PutImageChunk(context.Background(), "img-1", 16, []byte("chunk-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if ack.CommittedBytes != 27 || ack.Complete {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}

func TestPutImageChunkSurfacesAPIError(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"detail":{"detail":"gap","committed_bytes":8}}`))
	})
	defer srv.Close()

	_, err := c.PutImageChunk(context.Background(), "img-1", 99, []byte("x"))
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusConflict {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetImageParsesStatus(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/img-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"id":"img-1","status":"verifying","size_bytes":10,"committed_bytes":10,` +
			`"digest":"sha256:aa","image_ref":null,"error":null,"failed_phase":null,` +
			`"nonce_matched":null,"verify_reply_at":null,"ready_at":null,` +
			`"created_at":"2026-07-03T00:00:00Z"}`))
	})
	defer srv.Close()

	img, err := c.GetImage(context.Background(), "img-1")
	if err != nil {
		t.Fatal(err)
	}
	if img.Status != "verifying" || img.Digest == nil || *img.Digest != "sha256:aa" || img.ImageRef != nil {
		t.Fatalf("unexpected image: %+v", img)
	}
}

func TestVerifyImageUsesLongTimeoutClient(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/verify") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"id":"img-1","status":"ready","size_bytes":10,"committed_bytes":10,` +
			`"created_at":"2026-07-03T00:00:00Z"}`))
	})
	defer srv.Close()

	img, err := c.VerifyImage(context.Background(), "img-1")
	if err != nil {
		t.Fatal(err)
	}
	if img.Status != "ready" {
		t.Fatalf("unexpected image: %+v", img)
	}
	// The default client keeps its short timeout — VerifyImage must not have
	// mutated it while building its long-timeout variant.
	if c.HTTP.Timeout >= 30*60*1e9 {
		t.Fatalf("VerifyImage mutated the shared client timeout: %v", c.HTTP.Timeout)
	}
}

func TestBuiltinImagesParsesCatalog(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/builtin" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"images":[
			{"name":"zeroclaw","description":"Stock ZeroClaw","pod_size":"small","available":true,"default":false,"daily_fee_dollars":0.5},
			{"name":"openclaw","description":"OpenClaw","pod_size":"medium","available":false,"default":false,"daily_fee_dollars":2.0}
		],"custom_images":[
			{"name":"research","pod_size":"medium","default":true,"daily_fee_dollars":2.0,"ready_at":"2026-07-03T00:00:00Z"}
		],"default_image":"research"}`))
	})
	defer srv.Close()

	catalog, err := c.BuiltinImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	images := catalog.Images
	if len(images) != 2 || images[0].Name != "zeroclaw" || !images[0].Available {
		t.Fatalf("unexpected catalog: %+v", images)
	}
	if images[1].Available || images[1].PodSize != "medium" || images[1].DailyFeeDollars != 2.0 {
		t.Fatalf("unexpected openclaw entry: %+v", images[1])
	}
	if catalog.DefaultImage != "research" {
		t.Fatalf("default image not parsed: %+v", catalog)
	}
	if len(catalog.CustomImages) != 1 || catalog.CustomImages[0].Name != "research" ||
		!catalog.CustomImages[0].Default || catalog.CustomImages[0].ReadyAt == nil {
		t.Fatalf("unexpected custom images: %+v", catalog.CustomImages)
	}
}

func TestFinalizeImagePostsToFinalize(t *testing.T) {
	var method, path string
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Write([]byte(`{"id":"img_1","status":"uploaded"}`))
	})
	defer srv.Close()

	got, err := c.FinalizeImage(context.Background(), "img_1")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/v1/images/img_1/finalize" {
		t.Errorf("unexpected request: %s %s", method, path)
	}
	if got.Status != "uploaded" {
		t.Errorf("status = %q", got.Status)
	}
}

func TestCurrentImageParsesReadyAt(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/images/current" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"id":"img_9","status":"ready","pod_size":"large",
			"image_ref":"reg/img@sha256:abc","ready_at":"2026-01-02T03:04:05Z"}`))
	})
	defer srv.Close()

	got, err := c.CurrentImage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "img_9" || got.Status != "ready" || got.PodSize != "large" {
		t.Fatalf("unexpected image: %+v", got)
	}
	if got.ImageRef == nil || *got.ImageRef != "reg/img@sha256:abc" {
		t.Errorf("image_ref = %v", got.ImageRef)
	}
	if got.ReadyAt == nil || !got.ReadyAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("ready_at = %v", got.ReadyAt)
	}
}

// An account that has never pushed a custom image gets a 404; the caller must
// see it as an APIError rather than a zero-valued Image.
func TestCurrentImageSurfacesNotFound(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"no custom image"}`))
	})
	defer srv.Close()

	got, err := c.CurrentImage(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if got != nil {
		t.Errorf("image = %+v, want nil", got)
	}
}
