package api

import (
	"context"
	"fmt"
	"net/url"
)

// ExtraFile describes a non-weights file included in an upload (README, config.json, samples).
type ExtraFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// InitiateBody requests presigned upload targets.
type InitiateBody struct {
	SHA256          string      `json:"sha256"`
	Size            int64       `json:"size"`
	Filename        string      `json:"filename"`
	ExplicitVersion *string     `json:"explicit_version"`
	ExtraFiles      []ExtraFile `json:"extra_files"`
}

// Part is one presigned part of a multipart upload.
type Part struct {
	PartNumber int    `json:"part_number"`
	URL        string `json:"url"`
	Size       int64  `json:"size"`
}

// UploadTarget is a presigned destination for a single file.
type UploadTarget struct {
	Name        string            `json:"name"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Parts       []Part            `json:"parts"`        // multipart (large files)
	CompleteURL string            `json:"complete_url"` // multipart completion
}

// InitiateResp is the presigned-upload plan.
type InitiateResp struct {
	UploadID    string         `json:"upload_id"`
	Uploads     []UploadTarget `json:"uploads"`
	DuplicateOf *string        `json:"duplicate_of"`
}

// PartETag is a completed multipart part.
type PartETag struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

// FinalizeBody commits the version after upload.
type FinalizeBody struct {
	UploadID string     `json:"upload_id"`
	SHA256   string     `json:"sha256"`
	Parts    []PartETag `json:"parts"`
}

// FinalizeResp returns the assigned version tag.
type FinalizeResp struct {
	Version string `json:"version"`
}

// DownloadFile is a presigned GET target with integrity metadata.
type DownloadFile struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// DownloadResp lists files to download for a version.
type DownloadResp struct {
	Version string         `json:"version"`
	Files   []DownloadFile `json:"files"`
}

// InitiateUpload requests presigned upload URLs (non-idempotent).
func (c *Client) InitiateUpload(ctx context.Context, owner, repo string, body InitiateBody) (*InitiateResp, error) {
	var out InitiateResp
	if err := c.post(ctx, fmt.Sprintf("/v1/repos/%s/%s/versions:initiate", owner, repo), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FinalizeUpload commits the version. Never auto-retried.
func (c *Client) FinalizeUpload(ctx context.Context, owner, repo string, body FinalizeBody) (*FinalizeResp, error) {
	var out FinalizeResp
	if err := c.post(ctx, fmt.Sprintf("/v1/repos/%s/%s/versions:finalize", owner, repo), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Download requests presigned GET URLs. include = "weights" or "all" (+samples handled server-side).
func (c *Client) Download(ctx context.Context, owner, repo, version, include string) (*DownloadResp, error) {
	q := url.Values{}
	setStr(q, "version", version)
	setStr(q, "include", include)
	var out DownloadResp
	if err := c.get(ctx, fmt.Sprintf("/v1/repos/%s/%s/download?%s", owner, repo, q.Encode()), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
