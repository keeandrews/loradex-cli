package api

import "context"

// Me is the authenticated identity + storage usage.
type Me struct {
	Handle       string `json:"handle"`
	Plan         string `json:"plan"`
	StorageUsed  int64  `json:"storage_used"`
	StorageQuota int64  `json:"storage_quota"`
}

// Me returns the current identity (validates the token).
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var m Me
	if err := c.get(ctx, "/v1/me", &m); err != nil {
		return nil, err
	}
	return &m, nil
}
