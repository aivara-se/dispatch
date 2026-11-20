package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Block adds the blocked label to a pull request through an installation
// token. An error names the pull request and the status, never the token.
func (c *Client) Block(ctx context.Context, owner, repo string, number int) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", url.PathEscape(owner), url.PathEscape(repo), number)
	err := c.read(ctx, owner, repo, http.MethodPost, path, []byte(`{"labels":["blocked"]}`), nil)
	if err == nil {
		return nil
	}
	return fmt.Errorf("github: label %s/%s#%d: %s", owner, repo, number,
		strings.TrimPrefix(err.Error(), "github: "+path+": "))
}
