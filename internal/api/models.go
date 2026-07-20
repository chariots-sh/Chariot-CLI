package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CatalogModel is one curated hosted-model entry — a model Chariot knows how
// to serve on a single dedicated GPU (`chariot models catalog`).
type CatalogModel struct {
	CatalogID     string `json:"catalog_id"`
	HFRepo        string `json:"hf_repo"`
	GpuTier       string `json:"gpu_tier"`
	GpuHourMicros int64  `json:"gpu_hour_micros"`
	MaxModelLen   int64  `json:"max_model_len"`
	Description   string `json:"description"`
}

// ModelCatalogResponse also carries the per-tier GPU-hour prices, for the
// custom-model tier picker.
type ModelCatalogResponse struct {
	Models             []CatalogModel   `json:"models"`
	GpuHourMicrosByTier map[string]int64 `json:"gpu_hour_micros_by_tier"`
}

// HostedModel is the backend's view of one hosted-model row — both its
// artifact lifecycle (push → verify) and its serving lifecycle (host → warm
// ⇄ idle → drop).
type HostedModel struct {
	ModelID          string  `json:"model_id"`
	Name             string  `json:"name"`
	Source           string  `json:"source"`
	Status           string  `json:"status"`
	GpuTier          string  `json:"gpu_tier"`
	GpuHourMicros    int64   `json:"gpu_hour_micros"`
	CatalogID        *string `json:"catalog_id"`
	HFRepo           *string `json:"hf_repo"`
	ServingState     string  `json:"serving_state"`
	ServingMode      string  `json:"serving_mode"`
	IdleAfterSeconds *int64  `json:"idle_after_seconds"`
	SizeBytes        *int64  `json:"size_bytes"`
	CommittedBytes   int64   `json:"committed_bytes"`
	VerifyGpuSeconds *int64  `json:"verify_gpu_seconds"`
	Error            *string `json:"error"`
	FailedPhase      *string `json:"failed_phase"`
}

// ModelCatalog fetches the curated hosted-model catalog and tier prices.
func (c *Client) ModelCatalog(ctx context.Context) (*ModelCatalogResponse, error) {
	out := &ModelCatalogResponse{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/models/catalog", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterModelParams mirrors POST /v1/models. Source-specific fields stay
// empty when unused.
type RegisterModelParams struct {
	Name      string   `json:"name"`
	Source    string   `json:"source"` // catalog | hf | upload | adapter
	CatalogID string   `json:"catalog_id,omitempty"`
	HFRepo    string   `json:"hf_repo,omitempty"`
	HFToken   string   `json:"hf_token,omitempty"`
	GpuTier   string   `json:"gpu_tier,omitempty"`
	SizeBytes int64    `json:"size_bytes,omitempty"`
	SHA256    string   `json:"sha256,omitempty"`
	Manifest  []string `json:"manifest,omitempty"`
	Replace   bool     `json:"replace,omitempty"`
}

// ModelRegister is the backend's response to registering a model push.
type ModelRegister struct {
	ModelID        string `json:"model_id"`
	ChunkSizeBytes int64  `json:"chunk_size_bytes"`
	Status         string `json:"status"`
}

// RegisterModel creates the hosted-model row (and, for uploads, the chunked
// upload session).
func (c *Client) RegisterModel(ctx context.Context, params RegisterModelParams) (*ModelRegister, error) {
	out := &ModelRegister{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/models", params, out); err != nil {
		return nil, err
	}
	return out, nil
}

// PutModelChunk uploads one raw chunk at the given byte offset — the same
// resumable contract as image chunks (the ack's committed count is
// authoritative).
func (c *Client) PutModelChunk(ctx context.Context, modelID string, offset int64, data []byte) (*ChunkAck, error) {
	url := fmt.Sprintf("%s/v1/models/%s/chunks/%d", c.BaseURL, modelID, offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, &APIError{Status: resp.StatusCode, Detail: extractDetail(raw)}
	}
	out := &ChunkAck{}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("decoding chunk ack: %w", err)
	}
	return out, nil
}

// FinalizeModel completes the upload (all chunks in).
func (c *Client) FinalizeModel(ctx context.Context, modelID string) (*HostedModel, error) {
	out := &HostedModel{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/models/"+modelID+"/finalize", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetModel polls one hosted-model row (the verify progress target).
func (c *Client) GetModel(ctx context.Context, modelID string) (*HostedModel, error) {
	out := &HostedModel{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/models/"+modelID, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// VerifyModel drives the (long) verification pipeline; the caller polls
// GetModel concurrently for progress.
func (c *Client) VerifyModel(ctx context.Context, modelID string) (*HostedModel, error) {
	out := &HostedModel{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/models/"+modelID+"/verify", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListModels lists the account's hosted-model rows (any status).
func (c *Client) ListModels(ctx context.Context) ([]HostedModel, error) {
	out := &struct {
		Models []HostedModel `json:"models"`
	}{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/models", nil, out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

// HostModel starts serving a verified model. servingMode is scale_to_zero or
// always_on; idleAfterSeconds 0 keeps the server default.
func (c *Client) HostModel(ctx context.Context, name, servingMode string, idleAfterSeconds int64) (*HostedModel, error) {
	body := map[string]any{"serving_mode": servingMode}
	if idleAfterSeconds > 0 {
		body["idle_after_seconds"] = idleAfterSeconds
	}
	out := &HostedModel{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/models/named/"+name+"/host", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DropModelResult reports how many agents a drop's fallback re-pointed.
type DropModelResult struct {
	AgentsRepointed int64 `json:"agents_repointed"`
}

// DropModel stops serving a model. fallback (optional) re-points any agents
// and the account default that still use self/<name> in the same step.
func (c *Client) DropModel(ctx context.Context, name, fallback string) (*DropModelResult, error) {
	body := map[string]any{}
	if fallback != "" {
		body["fallback_model"] = fallback
	}
	out := &DropModelResult{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/models/named/"+name+"/drop", body, out); err != nil {
		return nil, err
	}
	return out, nil
}
