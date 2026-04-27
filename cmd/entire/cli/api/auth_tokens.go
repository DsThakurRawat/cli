package api

import (
	"context"
	"fmt"
)

// RevokeCurrentToken revokes the bearer token used to authenticate this client.
// Backed by DELETE /api/v1/auth/tokens/current.
func (c *Client) RevokeCurrentToken(ctx context.Context) error {
	resp, err := c.Delete(ctx, "/api/v1/auth/tokens/current")
	if err != nil {
		return fmt.Errorf("revoke current token: %w", err)
	}
	defer resp.Body.Close()

	if err := CheckResponse(resp); err != nil {
		return fmt.Errorf("revoke current token: %w", err)
	}
	return nil
}
