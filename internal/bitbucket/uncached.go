package bitbucket

// Uncached returns a client that reads past the response cache, for
// decisions that must see a pull request's current head commit and state.
// Its writes still clear the cache, and it shares c's HTTP client and
// credentials.
func (c *Client) Uncached() *Client {
	clone := *c
	if c.cache != nil {
		clone.cache = c.cache.InvalidateOnly()
	}
	return &clone
}
