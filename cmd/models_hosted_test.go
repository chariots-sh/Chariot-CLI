package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const catalogJSON = `{
  "models": [
    {"catalog_id": "gemma-4-12b", "hf_repo": "google/gemma-4-12b-it",
     "gpu_tier": "l4", "gpu_hour_micros": 770000, "max_model_len": 32768,
     "description": "Google Gemma 4 12B instruct — fastest, cheapest."},
    {"catalog_id": "qwen3.6-35b-a3b", "hf_repo": "Qwen/Qwen3.6-35B-A3B-Instruct-AWQ",
     "gpu_tier": "a100-80", "gpu_hour_micros": 5580000, "max_model_len": 131072,
     "description": "Qwen3.6 35B-A3B MoE — best agentic model per dollar."}
  ],
  "gpu_hour_micros_by_tier": {"l4": 770000, "a100-80": 5580000, "h100": 12170000, "h200": 10250000}
}`

func TestModelsCatalogRendersTiersAndPrices(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/catalog" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(catalogJSON))
	})

	got := runCLI(t, "", "models", "catalog")
	if got.err != nil {
		t.Fatalf("models catalog: %v", got.err)
	}
	mustContain(t, got.stdout, "gemma-4-12b", "stdout")
	mustContain(t, got.stdout, "$0.77", "stdout")
	mustContain(t, got.stdout, "chariot models host", "stdout")
}

// `models push --hf` registers the repo (no upload) and drives the verify
// request, surfacing the verified verdict + the host hint.
func TestModelsPushHFRegistersAndVerifies(t *testing.T) {
	var registered map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/models":
			_ = json.NewDecoder(r.Body).Decode(&registered)
			_, _ = w.Write([]byte(`{"model_id": "m-1", "chunk_size_bytes": 16777216, "status": "uploaded"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/models/m-1/verify":
			_, _ = w.Write([]byte(`{"model_id": "m-1", "name": "my-ft", "source": "hf", "status": "verified",
				"gpu_tier": "a100-80", "gpu_hour_micros": 5580000, "serving_state": "stopped",
				"serving_mode": "scale_to_zero", "committed_bytes": 0, "verify_gpu_seconds": 240}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models/m-1":
			_, _ = w.Write([]byte(`{"model_id": "m-1", "name": "my-ft", "source": "hf", "status": "verified",
				"gpu_tier": "a100-80", "gpu_hour_micros": 5580000, "serving_state": "stopped",
				"serving_mode": "scale_to_zero", "committed_bytes": 0, "verify_gpu_seconds": 240}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "", "models", "push", "my-ft", "--hf", "myorg/my-finetune", "--hf-token", "hf_x", "--gpu", "a100-80")
	if got.err != nil {
		t.Fatalf("models push --hf: %v", got.err)
	}
	if registered["source"] != "hf" || registered["hf_repo"] != "myorg/my-finetune" {
		t.Errorf("registration body = %v", registered)
	}
	if registered["hf_token"] != "hf_x" || registered["gpu_tier"] != "a100-80" {
		t.Errorf("registration body = %v", registered)
	}
	mustContain(t, got.stdout, "✓ verified on a100-80", "stdout")
	mustContain(t, got.stdout, "chariot models host my-ft", "stdout")
}

// A checkpoint-directory push tars the dir, sends the safetensors manifest,
// chunks the bytes, finalizes, then verifies.
func TestModelsPushDirectoryUploadsManifestAndChunks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var registered map[string]any
	var chunks, finalized int
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/models":
			_ = json.NewDecoder(r.Body).Decode(&registered)
			_, _ = w.Write([]byte(`{"model_id": "m-2", "chunk_size_bytes": 16777216, "status": "uploading"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/models/m-2/chunks/0":
			chunks++
			size := int64(registered["size_bytes"].(float64))
			_ = json.NewEncoder(w).Encode(map[string]any{"committed_bytes": size, "complete": true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/models/m-2/finalize":
			finalized++
			_, _ = w.Write([]byte(`{"model_id": "m-2", "name": "up", "source": "upload", "status": "uploaded",
				"gpu_tier": "l4", "gpu_hour_micros": 770000, "serving_state": "stopped",
				"serving_mode": "scale_to_zero", "committed_bytes": 7}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/models/m-2/verify":
			_, _ = w.Write([]byte(`{"model_id": "m-2", "name": "up", "source": "upload", "status": "verified",
				"gpu_tier": "l4", "gpu_hour_micros": 770000, "serving_state": "stopped",
				"serving_mode": "scale_to_zero", "committed_bytes": 7, "verify_gpu_seconds": 60}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models/m-2":
			_, _ = w.Write([]byte(`{"model_id": "m-2", "name": "up", "source": "upload", "status": "verified",
				"gpu_tier": "l4", "gpu_hour_micros": 770000, "serving_state": "stopped",
				"serving_mode": "scale_to_zero", "committed_bytes": 7, "verify_gpu_seconds": 60}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "", "models", "push", "up", dir, "--gpu", "l4")
	if got.err != nil {
		t.Fatalf("models push dir: %v", got.err)
	}
	manifest, _ := registered["manifest"].([]any)
	if len(manifest) != 2 || manifest[0] != "config.json" || manifest[1] != "model.safetensors" {
		t.Errorf("manifest = %v", manifest)
	}
	if registered["source"] != "upload" || registered["gpu_tier"] != "l4" {
		t.Errorf("registration body = %v", registered)
	}
	if chunks == 0 || finalized != 1 {
		t.Errorf("chunks = %d, finalized = %d", chunks, finalized)
	}
	mustContain(t, got.stdout, "✓ verified on l4", "stdout")
}

// `models host <catalog-id>` auto-registers the pre-verified catalog entry
// (under its sanitized name) and asks for billing consent first.
func TestModelsHostCatalogEntryConfirmsAndHosts(t *testing.T) {
	var registered map[string]any
	var hostPath string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"models": []}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models/catalog":
			_, _ = w.Write([]byte(catalogJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/models":
			_ = json.NewDecoder(r.Body).Decode(&registered)
			_, _ = w.Write([]byte(`{"model_id": "m-3", "chunk_size_bytes": 16777216, "status": "verified"}`))
		case r.Method == http.MethodPost:
			hostPath = r.URL.Path
			_, _ = w.Write([]byte(`{"model_id": "m-3", "name": "qwen3-6-35b-a3b", "source": "catalog",
				"status": "verified", "gpu_tier": "a100-80", "gpu_hour_micros": 5580000,
				"serving_state": "starting", "serving_mode": "scale_to_zero", "committed_bytes": 0}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "y\n", "models", "host", "qwen3.6-35b-a3b")
	if got.err != nil {
		t.Fatalf("models host: %v", got.err)
	}
	if registered["source"] != "catalog" || registered["catalog_id"] != "qwen3.6-35b-a3b" {
		t.Errorf("registration body = %v", registered)
	}
	if registered["name"] != "qwen3-6-35b-a3b" { // dots sanitized for the Service name
		t.Errorf("registered name = %v", registered["name"])
	}
	if hostPath != "/v1/models/named/qwen3-6-35b-a3b/host" {
		t.Errorf("host path = %q", hostPath)
	}
	mustContain(t, got.stdout, "$5.58/GPU-hr", "stdout")
	mustContain(t, got.stdout, "Proceed? [y/N]", "stdout")
	mustContain(t, got.stdout, "chariot models set self/qwen3-6-35b-a3b", "stdout")
}

// Declining a CATALOG host leaves no state behind: no registration row is
// created and no host call is made — consent gates every mutation.
func TestModelsHostCatalogDeclineRegistersNothing(t *testing.T) {
	var mutations int
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"models": []}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models/catalog":
			_, _ = w.Write([]byte(catalogJSON))
		case r.Method == http.MethodPost:
			mutations++
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "n\n", "models", "host", "gemma-4-12b")
	if got.err != nil {
		t.Fatalf("models host (declined catalog): %v", got.err)
	}
	if mutations != 0 {
		t.Errorf("%d mutation(s) despite declined consent", mutations)
	}
	mustContain(t, got.stdout, "aborted", "stdout")
}

// Declining the billing prompt aborts without any host call.
func TestModelsHostAbortsWithoutConsent(t *testing.T) {
	var hostCalls int
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"models": [{"model_id": "m-4", "name": "my-ft", "source": "hf",
				"status": "verified", "gpu_tier": "h100", "gpu_hour_micros": 12170000,
				"serving_state": "stopped", "serving_mode": "scale_to_zero", "committed_bytes": 0}]}`))
		case r.Method == http.MethodPost:
			hostCalls++
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "n\n", "models", "host", "my-ft")
	if got.err != nil {
		t.Fatalf("models host (declined): %v", got.err)
	}
	if hostCalls != 0 {
		t.Errorf("host was called despite declined consent")
	}
	mustContain(t, got.stdout, "aborted", "stdout")
}

func TestModelsHostedListsStateAndRates(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models": [
			{"model_id": "m-1", "name": "my-qwen", "source": "catalog", "status": "verified",
			 "gpu_tier": "a100-80", "gpu_hour_micros": 5580000, "serving_state": "warm",
			 "serving_mode": "scale_to_zero", "committed_bytes": 0},
			{"model_id": "m-2", "name": "my-gemma", "source": "catalog", "status": "verified",
			 "gpu_tier": "l4", "gpu_hour_micros": 770000, "serving_state": "idle",
			 "serving_mode": "scale_to_zero", "committed_bytes": 0}
		]}`))
	})

	got := runCLI(t, "", "models", "hosted")
	if got.err != nil {
		t.Fatalf("models hosted: %v", got.err)
	}
	mustContain(t, got.stdout, "self/my-qwen", "stdout")
	mustContain(t, got.stdout, "warm", "stdout")
	mustContain(t, got.stdout, "idle", "stdout")
	mustContain(t, got.stdout, "$5.58", "stdout")
}

// `models drop --fallback` sends the fallback and reports the re-point.
func TestModelsDropSendsFallback(t *testing.T) {
	var body map[string]any
	var path string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"agents_repointed": 3}`))
	})

	got := runCLI(t, "", "models", "drop", "self/my-qwen", "--fallback", "deepseek/deepseek-v4-flash")
	if got.err != nil {
		t.Fatalf("models drop: %v", got.err)
	}
	if path != "/v1/models/named/my-qwen/drop" {
		t.Errorf("path = %q", path)
	}
	if body["fallback_model"] != "deepseek/deepseek-v4-flash" {
		t.Errorf("body = %v", body)
	}
	mustContain(t, got.stdout, "✓ dropped self/my-qwen — billing stopped", "stdout")
}
