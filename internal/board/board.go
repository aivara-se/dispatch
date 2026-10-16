// Package board reads and writes the board: the GitHub project whose Status
// field is each card's column, and the issues that are its cards. The whole
// board is read with the GraphQL project-items query, one page at a time;
// one card is read, and its assignee written, with the REST issue endpoint,
// which answers an issue and a pull request alike.
package board

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Card is one card on the board: the issue's number, the column the
// project's Status field reports, and the login that holds it. Column and
// Assignee are empty when the card has none.
type Card struct {
	Number   int
	Column   string
	Assignee string
}

// Client is the board's one client: the GraphQL project read and the REST
// issue endpoint, both carrying the token the client is built with. It
// writes nothing anywhere but a card's assignee.
type Client struct {
	httpClient *http.Client
	graphqlURL string
	apiBase    string
}

// New builds the board's client from the one token both surfaces use, fixed
// here at boot. The endpoints stay unexported so tests can point them at a
// stub server.
func New(token string) *Client {
	return &Client{
		httpClient: &http.Client{Transport: tokenTransport{token: token, next: http.DefaultTransport}},
		graphqlURL: "https://api.github.com/graphql",
		apiBase:    "https://api.github.com",
	}
}

// ReadAll reads the whole board — every card, its column and its assignee —
// one page at a time, in the board's order. A board that holds no cards is
// an empty answer, not a failed read; a board that cannot be read is an
// error, never an empty one.
func (c *Client) ReadAll(ctx context.Context, owner string, project int) ([]Card, error) {
	var cards []Card
	cursor := ""
	for {
		page, next, err := c.projectPage(ctx, owner, project, cursor)
		if err != nil {
			return nil, err
		}
		cards = append(cards, page...)
		if next == "" {
			return cards, nil
		}
		if next == cursor {
			return nil, fmt.Errorf("read board: the pages do not advance")
		}
		cursor = next
	}
}

// Assignee reads one card's assignee. A card that is not there and a card
// nobody holds are both absent — the empty answer — while a read that fails
// is an error: absence never stands in for failure.
func (c *Client) Assignee(ctx context.Context, owner, repo string, number int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.issueURL(owner, repo, number), nil)
	if err != nil {
		return "", fmt.Errorf("read card %d: %w", number, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("read card %d: %w", number, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read card %d: the issue endpoint answered %d", number, resp.StatusCode)
	}
	var issue struct {
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return "", fmt.Errorf("read card %d: decode: %w", number, err)
	}
	if len(issue.Assignees) == 0 {
		return "", nil
	}
	return issue.Assignees[0].Login, nil
}

// Assign writes one card's assignee — the only field the service writes
// anywhere, through the endpoint the project shows. A write that fails is
// an error the caller refuses the delivery with, never an answer that
// reads as nobody woken.
func (c *Client) Assign(ctx context.Context, owner, repo string, number int, login string) error {
	body, err := json.Marshal(map[string][]string{"assignees": {login}})
	if err != nil {
		return fmt.Errorf("assign card %d: %w", number, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.issueURL(owner, repo, number), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("assign card %d: %w", number, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("assign card %d: %w", number, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("assign card %d: the issue endpoint answered %d", number, resp.StatusCode)
	}
	return nil
}

// projectQuery reads one page of the board. It is a query and never a
// mutation: the client's only write is Assign, through the REST endpoint.
const projectQuery = `query($owner: String!, $number: Int!, $cursor: String) {
  organization(login: $owner) {
    projectV2(number: $number) {
      items(first: 20, after: $cursor) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          content {
            ... on Issue {
              number
              assignees(first: 1) {
                nodes {
                  login
                }
              }
            }
            ... on PullRequest {
              number
              assignees(first: 1) {
                nodes {
                  login
                }
              }
            }
          }
          fieldValueByName(name: "Status") {
            ... on ProjectV2ItemFieldSingleSelectValue {
              name
            }
          }
        }
      }
    }
  }
}`

// projectPage reads one page of the project's items and returns its cards
// with the cursor the next page follows, empty at the last page.
func (c *Client) projectPage(ctx context.Context, owner string, project int, cursor string) ([]Card, string, error) {
	variables := map[string]any{"owner": owner, "number": project, "cursor": nil}
	if cursor != "" {
		variables["cursor"] = cursor
	}
	body, err := json.Marshal(graphqlRequest{Query: projectQuery, Variables: variables})
	if err != nil {
		return nil, "", fmt.Errorf("read board: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("read board: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("read board: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("read board: graphql answered %d", resp.StatusCode)
	}
	var page graphqlPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, "", fmt.Errorf("read board: decode: %w", err)
	}
	if len(page.Errors) > 0 {
		return nil, "", fmt.Errorf("read board: graphql answered with %d errors, the first of type %q", len(page.Errors), page.Errors[0].Type)
	}
	if page.Data.Organization == nil {
		return nil, "", fmt.Errorf("read board: organization %s is absent", owner)
	}
	if page.Data.Organization.ProjectV2 == nil {
		return nil, "", fmt.Errorf("read board: project %d is absent for %s", project, owner)
	}
	items := page.Data.Organization.ProjectV2.Items
	if items.PageInfo.HasNextPage && items.PageInfo.EndCursor == "" {
		return nil, "", fmt.Errorf("read board: a page with more pages names no cursor")
	}
	cards := make([]Card, 0, len(items.Nodes))
	for _, node := range items.Nodes {
		// A draft item is not a card: it has no issue to read, assign, or
		// wake an agent for.
		if node.Content.Number == 0 {
			continue
		}
		card := Card{Number: node.Content.Number}
		if len(node.Content.Assignees.Nodes) > 0 {
			card.Assignee = node.Content.Assignees.Nodes[0].Login
		}
		if node.Status != nil {
			card.Column = node.Status.Name
		}
		cards = append(cards, card)
	}
	if items.PageInfo.HasNextPage {
		return cards, items.PageInfo.EndCursor, nil
	}
	return cards, "", nil
}

// graphqlRequest is one GraphQL call: the fixed query and its variables.
type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// graphqlPage is one page's answer, with only the fields the query asks for
// modelled. An error keeps its type and drops its message: the message is
// GitHub's text, and the service's records carry its own words.
type graphqlPage struct {
	Data struct {
		Organization *struct {
			ProjectV2 *struct {
				Items struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Content struct {
							Number    int `json:"number"`
							Assignees struct {
								Nodes []struct {
									Login string `json:"login"`
								} `json:"nodes"`
							} `json:"assignees"`
						} `json:"content"`
						Status *struct {
							Name string `json:"name"`
						} `json:"fieldValueByName"`
					} `json:"nodes"`
				} `json:"items"`
			} `json:"projectV2"`
		} `json:"organization"`
	} `json:"data"`
	Errors []struct {
		Type string `json:"type"`
	} `json:"errors"`
}

// issueURL is the REST endpoint of one card: the issue endpoint, which
// answers an issue and a pull request alike.
func (c *Client) issueURL(owner, repo string, number int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.apiBase, owner, repo, number)
}

// tokenTransport carries the board token on every request either surface
// makes, so the token is read once, at boot, where the client is built.
type tokenTransport struct {
	token string
	next  http.RoundTripper
}

// RoundTrip sends one request with the token's Authorization header.
func (t tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.next.RoundTrip(clone)
}
