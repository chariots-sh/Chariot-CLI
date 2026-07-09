package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Image is the backend's view of a custom agent image upload/verification —
// the poll target `chariot image push` renders its progress from.
type Image struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	SizeBytes      int64      `json:"size_bytes"`
	CommittedBytes int64      `json:"committed_bytes"`
	PodSize        string     `json:"pod_size"`
	Digest         *string    `json:"digest"`
	ImageRef       *string    `json:"image_ref"`
	Error          *string    `json:"error"`
	FailedPhase    *string    `json:"failed_phase"`
	NonceMatched   *bool      `json:"nonce_matched"`
	VerifyReplyAt  *time.Time `json:"verify_reply_at"`
	ReadyAt        *time.Time `json:"ready_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// BuiltinImage is one entry of the built-in image catalog — the images
// Chariot offers out of the box, deployable per agent via `chariot deploy
// --image <name>`.
type BuiltinImage struct {
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	PodSize         string  `json:"pod_size"`
	Available       bool    `json:"available"` // false = published soon; not deployable yet
	Default         bool    `json:"default"`
	DailyFeeDollars float64 `json:"daily_fee_dollars"`
}

// CustomImage is one of the account's verified custom images — deployable by
// name exactly like a builtin (`chariot deploy --image <name>`).
type CustomImage struct {
	Name            string     `json:"name"`
	PodSize         string     `json:"pod_size"`
	Default         bool       `json:"default"`
	DailyFeeDollars float64    `json:"daily_fee_dollars"`
	ReadyAt         *time.Time `json:"ready_at"`
}

// ImageCatalog is everything the account can deploy: the built-in catalog,
// the account's verified custom images, and the effective default name.
type ImageCatalog struct {
	Images       []BuiltinImage `json:"images"`
	CustomImages []CustomImage  `json:"custom_images"`
	DefaultImage string         `json:"default_image"`
}

// BuiltinImages lists the deployable image catalog (`chariot images`) —
// builtins plus the account's verified custom images.
func (c *Client) BuiltinImages(ctx context.Context) (*ImageCatalog, error) {
	out := &ImageCatalog{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/images/builtin", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ImageCreate is the backend's response to starting an upload.
type ImageCreate struct {
	ImageID        string `json:"image_id"`
	ChunkSizeBytes int64  `json:"chunk_size_bytes"`
}

// CreateImage starts a chunked upload for a docker-save tarball of the given
// size/checksum. podSize picks the CPU/memory tier the image's agents run at
// (small | medium | large); name is what agents reference the image by
// (`deploy --image <name>` — pushing an existing name upgrades it). replace
// abandons an unfinished previous upload.
func (c *Client) CreateImage(ctx context.Context, sizeBytes int64, sha256 string, podSize, name string, replace bool) (*ImageCreate, error) {
	out := &ImageCreate{}
	body := map[string]any{
		"size_bytes": sizeBytes,
		"sha256":     sha256,
		"pod_size":   podSize,
		"name":       name,
		"replace":    replace,
	}
	if _, err := c.do(ctx, http.MethodPost, "/v1/images", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ChunkAck reports how many bytes the backend has committed after a chunk.
type ChunkAck struct {
	CommittedBytes int64 `json:"committed_bytes"`
	Complete       bool  `json:"complete"`
}

// PutImageChunk uploads one raw chunk at the given byte offset. A replayed
// chunk is not an error — the ack carries the authoritative committed count,
// so the caller can fast-forward (or resume after a 409 gap).
func (c *Client) PutImageChunk(ctx context.Context, imageID string, offset int64, data []byte) (*ChunkAck, error) {
	url := fmt.Sprintf("%s/v1/images/%s/chunks/%d", c.BaseURL, imageID, offset)
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

// FinalizeImage marks the upload complete (all bytes committed).
func (c *Client) FinalizeImage(ctx context.Context, imageID string) (*Image, error) {
	out := &Image{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/images/"+imageID+"/finalize", struct{}{}, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetImage fetches the image's current state (the progress-poll call).
func (c *Client) GetImage(ctx context.Context, imageID string) (*Image, error) {
	out := &Image{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/images/"+imageID, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// CurrentImage fetches the account's most recent image (for `image status`).
func (c *Client) CurrentImage(ctx context.Context) (*Image, error) {
	out := &Image{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/images/current", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// VerifyImage drives the backend's full verification pipeline. This is ONE
// long request (the backend pushes the image, spins up a test agent, waits for
// its reply, and tears everything down before responding), so it uses a
// dedicated client with a much longer timeout than the default 30s. Callers
// poll GetImage concurrently for progress.
func (c *Client) VerifyImage(ctx context.Context, imageID string) (*Image, error) {
	long := &Client{
		BaseURL: c.BaseURL,
		Token:   c.Token,
		HTTP:    &http.Client{Timeout: 30 * time.Minute},
	}
	out := &Image{}
	if _, err := long.do(ctx, http.MethodPost, "/v1/images/"+imageID+"/verify", struct{}{}, out); err != nil {
		return nil, err
	}
	return out, nil
}
