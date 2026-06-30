package api

import (
	"context"
	"net/url"
)

// SearchParams filters the search endpoint. base/format/tag are repeatable.
type SearchParams struct {
	Query  string
	Base   []string
	Format []string
	Tag    []string
	Sort   string
	Limit  int
	Page   int
}

// Search runs compatibility-first + full-text discovery.
func (c *Client) Search(ctx context.Context, p SearchParams) (*RepoList, error) {
	q := url.Values{}
	setStr(q, "q", p.Query)
	for _, b := range p.Base {
		q.Add("base", b)
	}
	for _, f := range p.Format {
		q.Add("format", f)
	}
	for _, t := range p.Tag {
		q.Add("tag", t)
	}
	setStr(q, "sort", p.Sort)
	setInt(q, "limit", p.Limit)
	setInt(q, "page", p.Page)
	var out RepoList
	if err := c.get(ctx, "/v1/search?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
