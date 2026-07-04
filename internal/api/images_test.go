package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"image_id":"img-1","chunk_size_bytes":16777216}`))
	})
	defer srv.Close()

	res, err := c.CreateImage(context.Background(), 42, "ab12", true)
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
