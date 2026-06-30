package api

import (
	"context"
	"fmt"
	"net/url"
)

// Repo is repository metadata. `readme` is present on detail responses only.
type Repo struct {
	Owner             string   `json:"owner"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Visibility        string   `json:"visibility"`
	BaseModel         string   `json:"base_model"`
	Format            string   `json:"format"`
	License           string   `json:"license"`
	TriggerWords      []string `json:"trigger_words"`
	NetworkRank       int      `json:"network_rank"`
	NetworkDim        int      `json:"network_dim"`
	RecommendedWeight float64  `json:"recommended_weight"`
	Tags              []string `json:"tags"`
	Downloads         int64    `json:"downloads"`
	Stars             int64    `json:"stars"`
	LatestVersion     string   `json:"latest_version"`
	Size              int64    `json:"size"`
	UpdatedAt         string   `json:"updated_at"`
	Readme            string   `json:"readme,omitempty"`
}

// Version is an immutable published version.
type Version struct {
	Tag       string `json:"tag"`
	CreatedAt string `json:"created_at"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Notes     string `json:"notes"`
}

// File is an entry within a version.
type File struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// RepoList is the standard paginated envelope.
type RepoList struct {
	Items []Repo `json:"items"`
	Page  int    `json:"page"`
	Total int    `json:"total"`
}

// ListReposParams filters the list endpoint.
type ListReposParams struct {
	Owner      string
	Visibility string
	Starred    bool
	Sort       string
	Limit      int
	Page       int
}

// ListRepos lists repositories.
func (c *Client) ListRepos(ctx context.Context, p ListReposParams) (*RepoList, error) {
	q := url.Values{}
	setStr(q, "owner", p.Owner)
	setStr(q, "visibility", p.Visibility)
	if p.Starred {
		q.Set("starred", "true")
	}
	setStr(q, "sort", p.Sort)
	setInt(q, "limit", p.Limit)
	setInt(q, "page", p.Page)
	var out RepoList
	if err := c.get(ctx, "/v1/repos?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRepo fetches full repo metadata (includes readme).
func (c *Client) GetRepo(ctx context.Context, owner, repo string) (*Repo, error) {
	var r Repo
	if err := c.get(ctx, fmt.Sprintf("/v1/repos/%s/%s", owner, repo), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListVersions returns the version history.
func (c *Client) ListVersions(ctx context.Context, owner, repo string) ([]Version, error) {
	var out struct {
		Items []Version `json:"items"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/repos/%s/%s/versions", owner, repo), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// ListFiles returns the files of a version.
func (c *Client) ListFiles(ctx context.Context, owner, repo, version string) (string, []File, error) {
	q := url.Values{}
	setStr(q, "version", version)
	var out struct {
		Version string `json:"version"`
		Items   []File `json:"items"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/repos/%s/%s/files?%s", owner, repo, q.Encode()), &out); err != nil {
		return "", nil, err
	}
	return out.Version, out.Items, nil
}

// CreateRepoBody is the metadata for creating a repo.
type CreateRepoBody struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Visibility        string   `json:"visibility"`
	BaseModel         string   `json:"base_model"`
	Format            string   `json:"format"`
	License           string   `json:"license"`
	TriggerWords      []string `json:"trigger_words"`
	NetworkRank       int      `json:"network_rank"`
	NetworkDim        int      `json:"network_dim"`
	RecommendedWeight float64  `json:"recommended_weight"`
	Tags              []string `json:"tags"`
}

// CreateRepo creates a repository. 409 if it exists.
func (c *Client) CreateRepo(ctx context.Context, owner string, body CreateRepoBody) (*Repo, error) {
	var r Repo
	// Owner is taken from the authenticated identity server-side; path scoping is by /v1/repos.
	if err := c.post(ctx, "/v1/repos", body, &r); err != nil {
		return nil, err
	}
	_ = owner
	return &r, nil
}

func setStr(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}
func setInt(q url.Values, k string, v int) {
	if v > 0 {
		q.Set(k, fmt.Sprintf("%d", v))
	}
}
