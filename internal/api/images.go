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

// SharedImage is an ACCEPTED share of another account's image — deployable
// by its alias exactly like a builtin (`chariot deploy --image <alias>`).
// Offers still pending acceptance appear only in ListShares.
type SharedImage struct {
	Name       string  `json:"name"` // the alias you deploy by
	OwnerEmail string  `json:"owner_email"`
	ImageName  string  `json:"image_name"` // the owner-side name
	PodSize    *string `json:"pod_size"`   // owner's current tier (nil mid re-push)
	// The tier you accepted — the fee ceiling; a re-push above it stops
	// resolving (status tier_raised) until you re-accept.
	AcceptedPodSize string   `json:"accepted_pod_size"`
	Default         bool     `json:"default"`
	DailyFeeDollars *float64 `json:"daily_fee_dollars"`
	// active | owner_repushing | tier_raised
	Status  string `json:"status"`
	ShareID string `json:"share_id"`
}

// ImageCatalog is everything the account can deploy: the built-in catalog,
// the account's verified custom images, images shared with the account, and
// the effective default name.
type ImageCatalog struct {
	Images       []BuiltinImage `json:"images"`
	CustomImages []CustomImage  `json:"custom_images"`
	SharedImages []SharedImage  `json:"shared_images"`
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

// OutgoingShare is a share the account offered/granted. Alias is nil until
// the grantee accepts; after that, re-pushes of the name flow to them
// automatically (up to the tier they accepted).
type OutgoingShare struct {
	ShareID      string  `json:"share_id"`
	ImageName    string  `json:"image_name"`
	GranteeEmail string  `json:"grantee_email"`
	Alias        *string `json:"alias"`
	// pending | active | owner_repushing | tier_raised
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// IncomingShare is a share the account received. Alias (the name it deploys
// the image by) and AcceptedPodSize are nil until the account accepts it.
type IncomingShare struct {
	ShareID         string  `json:"share_id"`
	Alias           *string `json:"alias"`
	OwnerEmail      string  `json:"owner_email"`
	ImageName       string  `json:"image_name"`
	PodSize         *string `json:"pod_size"`          // owner's current tier
	AcceptedPodSize *string `json:"accepted_pod_size"` // the fee ceiling accepted
	// pending | active | owner_repushing | tier_raised
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Shares is both directions of the account's image shares.
type Shares struct {
	Outgoing []OutgoingShare `json:"outgoing"`
	Incoming []IncomingShare `json:"incoming"`
}

// CreateShare offers one of the account's verified custom images to the
// account behind granteeEmail. Nothing changes for them until they accept
// (`chariot image accept` on their side).
func (c *Client) CreateShare(ctx context.Context, imageName, granteeEmail string) (*OutgoingShare, error) {
	body := map[string]any{
		"image_name":    imageName,
		"grantee_email": granteeEmail,
	}
	out := &OutgoingShare{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/images/shares", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// AcceptedShare is the backend's response to accepting a share.
type AcceptedShare struct {
	ShareID         string `json:"share_id"`
	Alias           string `json:"alias"`
	AcceptedPodSize string `json:"accepted_pod_size"`
}

// AcceptShare accepts a share offered to this account, binding the alias it
// deploys by (empty = the image's name) and locking in the current pod tier
// as the fee ceiling. Re-accepting approves a tier raise.
func (c *Client) AcceptShare(ctx context.Context, shareID, alias string) (*AcceptedShare, error) {
	body := map[string]any{}
	if alias != "" {
		body["alias"] = alias
	}
	out := &AcceptedShare{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/images/shares/"+shareID+"/accept", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListShares fetches both directions of the account's image shares.
func (c *Client) ListShares(ctx context.Context) (*Shares, error) {
	out := &Shares{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/images/shares", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteShare deletes a share the account is a party to — the owner revoking
// the grant, or the grantee removing an image shared with them. Agents still
// naming the alias fall back to the default resolution chain at their next
// wake.
func (c *Client) DeleteShare(ctx context.Context, shareID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/images/shares/"+shareID, nil, nil)
	return err
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
