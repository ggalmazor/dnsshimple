package dnsimple

import (
	"context"
	"encoding/json"
	"fmt"

	"dnsshimple/kit"
)

type pageEnvelope[Item any] struct {
	Data       []Item `json:"data"`
	Pagination struct {
		CurrentPage int `json:"current_page"`
		TotalPages  int `json:"total_pages"`
	} `json:"pagination"`
}

// listAll walks DNSimple's pagination to the last page, so an account with
// 300 zones sees 300 zones, not the first 100. Endpoints without pagination
// metadata read as a single page. The path must already carry a query string
// (per_page), because pages are appended with '&'.
func listAll[Item any](client *Client, ctx context.Context, accessToken, path, endpointLabel string) kit.Outcome[[]Item] {
	var collected []Item
	for page := 1; ; page++ {
		body := client.Get(ctx, accessToken, fmt.Sprintf("%s&page=%d", path, page), endpointLabel)
		if body.Failed() {
			return kit.Failure[[]Item](body.Error())
		}
		var envelope pageEnvelope[Item]
		if err := json.Unmarshal(body.Value(), &envelope); err != nil {
			return kit.Failure[[]Item](fmt.Errorf("decoding the %s page: %w", endpointLabel, err))
		}
		collected = append(collected, envelope.Data...)
		if page >= envelope.Pagination.TotalPages {
			return kit.Success(collected)
		}
	}
}
