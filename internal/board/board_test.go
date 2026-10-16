package board

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// pageOne and pageTwo are the two pages of a three-card board — card 75 is
// pull-request-numbered, the draft between the cards is not one, and card 72
// sits in no column.
const pageOne = `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":true,"endCursor":"page-2"},"nodes":[` +
	`{"content":{"number":69,"assignees":{"nodes":[{"login":"thani-sh-meme"}]},"repository":{"name":"dispatcher"}},"fieldValueByName":{"name":"Finished"}},` +
	`{"content":{"number":75,"assignees":{"nodes":[]},"repository":{"name":"dispatcher"}},"fieldValueByName":{"name":"Started"}},` +
	`{"content":{"title":"a draft note"},"fieldValueByName":null}` +
	`]}}}}}`

const pageTwo = `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":false,"endCursor":"the-end"},"nodes":[` +
	`{"content":{"number":71,"assignees":{"nodes":[{"login":"thani-sh-meme"}]},"repository":{"name":"other-repo"}},"fieldValueByName":{"name":"Refined"}},` +
	`{"content":{"number":72,"assignees":{"nodes":[]},"repository":{"name":"dispatcher"}},"fieldValueByName":null}` +
	`]}}}}}`

// emptyBoard is a board on which no card exists: an empty answer, not a
// failed read.
const emptyBoard = `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`

// serve starts a stub GitHub server answering with handler and returns a
// client pointed at it, both surfaces on the one stub.
func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	stub := httptest.NewServer(handler)
	t.Cleanup(stub.Close)
	client := New("board-token")
	client.graphqlURL = stub.URL + "/graphql"
	client.apiBase = stub.URL
	return client
}

// pages answers the project query with the page each cursor fixes, so a
// test follows the client from page to page.
func pages(pageByCursor map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		cursor := ""
		if value, ok := request.Variables["cursor"].(string); ok {
			cursor = value
		}
		page, ok := pageByCursor[cursor]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, page)
	}
}

// issue answers the REST issue endpoint with one status and body, whatever
// card was asked for.
func issue(status int, answer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, answer)
	}
}

func TestReadAllJoinsPagesIntoTheFullBoard(t *testing.T) {
	client := serve(t, pages(map[string]string{"": pageOne, "page-2": pageTwo}))
	got, err := client.ReadAll(context.Background(), "aivara-se", 2)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	want := []Card{
		{Number: 69, Column: "Finished", Assignee: "thani-sh-meme", Repo: "dispatcher"},
		{Number: 75, Column: "Started", Repo: "dispatcher"},
		{Number: 71, Column: "Refined", Assignee: "thani-sh-meme", Repo: "other-repo"},
		{Number: 72, Repo: "dispatcher"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the board read as %+v, want %+v", got, want)
	}
}

func TestReadAllReadsAnEmptyBoardAsAbsent(t *testing.T) {
	client := serve(t, pages(map[string]string{"": emptyBoard}))
	got, err := client.ReadAll(context.Background(), "aivara-se", 2)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the empty board read as %+v, want no cards", got)
	}
}

func TestReadAllRefuses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		answer string
		want   string
	}{
		{"a graphql call that answers 500", http.StatusInternalServerError, ``, "graphql answered 500"},
		{"a graphql answer with errors", http.StatusOK, `{"errors":[{"type":"NOT_FOUND"}],"data":{"organization":null}}`, "graphql answered with 1 errors"},
		{"an organization that is absent", http.StatusOK, `{"data":{}}`, "organization aivara-se is absent"},
		{"a project that is absent", http.StatusOK, `{"data":{"organization":{"projectV2":null}}}`, "project 2 is absent"},
		{"a page with more pages and no cursor", http.StatusOK, `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":true,"endCursor":""},"nodes":[]}}}}}`, "names no cursor"},
		{"a board whose pages do not advance", http.StatusOK, `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":true,"endCursor":"stuck"},"nodes":[]}}}}}`, "do not advance"},
		{"a card that names no repository", http.StatusOK, `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":false},"nodes":[{"content":{"number":73},"fieldValueByName":{"name":"Backlog"}}]}}}}}`, "names no repository"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				fmt.Fprint(w, c.answer)
			})
			_, err := client.ReadAll(context.Background(), "aivara-se", 2)
			if err == nil {
				t.Fatalf("read all accepted %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not say %q", err, c.want)
			}
		})
	}
}

func TestAssigneeReadsAPullRequestNumberedCard(t *testing.T) {
	client := serve(t, issue(http.StatusOK, `{"number":75,"assignees":[{"login":"thani-sh-mimi"}],"pull_request":{"url":"https://api.github.com/repos/aivara-se/dispatcher/pulls/75"}}`))
	login, err := client.Assignee(context.Background(), "aivara-se", "dispatcher", 75)
	if err != nil {
		t.Fatalf("read card: %v", err)
	}
	if login != "thani-sh-mimi" {
		t.Errorf("card 75 is held by %q, want thani-sh-mimi", login)
	}
}

func TestAssigneeReadsAbsenceAsAbsent(t *testing.T) {
	cases := []struct {
		name   string
		status int
		answer string
	}{
		{"a card nobody holds", http.StatusOK, `{"number":71,"assignees":[]}`},
		{"a card that is not there", http.StatusNotFound, `{"message":"Not Found"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := serve(t, issue(c.status, c.answer))
			login, err := client.Assignee(context.Background(), "aivara-se", "dispatcher", 71)
			if err != nil {
				t.Fatalf("read card: %v", err)
			}
			if login != "" {
				t.Errorf("absence read as %q, want nobody", login)
			}
		})
	}
}

func TestAssigneeRefusesAReadThatFails(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("a read answered %d", status), func(t *testing.T) {
			client := serve(t, issue(status, `{"message":"no"}`))
			_, err := client.Assignee(context.Background(), "aivara-se", "dispatcher", 71)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("answered %d", status)) {
				t.Errorf("a read answered %d came back as %v, want the failure named", status, err)
			}
		})
	}
}

func TestAssignRefusesAWriteThatFails(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("a write answered %d", status), func(t *testing.T) {
			client := serve(t, issue(status, `{"message":"no"}`))
			err := client.Assign(context.Background(), "aivara-se", "dispatcher", 71, "thani-sh-meme")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("answered %d", status)) {
				t.Errorf("a write answered %d came back as %v, want the failure named", status, err)
			}
		})
	}
}

func TestBothSurfacesCarryTheToken(t *testing.T) {
	var auths []string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, emptyBoard)
			return
		}
		fmt.Fprint(w, `{"number":71,"assignees":[]}`)
	})
	if _, err := client.ReadAll(context.Background(), "aivara-se", 2); err != nil {
		t.Fatalf("read all: %v", err)
	}
	if _, err := client.Assignee(context.Background(), "aivara-se", "dispatcher", 71); err != nil {
		t.Fatalf("read card: %v", err)
	}
	if err := client.Assign(context.Background(), "aivara-se", "dispatcher", 71, "thani-sh-meme"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if len(auths) != 3 {
		t.Fatalf("the client made %d requests, want 3", len(auths))
	}
	for _, auth := range auths {
		if auth != "Bearer board-token" {
			t.Errorf("a request carried %q, want the board token", auth)
		}
	}
}

// TestTheOnlyWriteIsTheAssignee exercises every surface and then reads the
// stub's record: the one request that was not a read is a single PATCH of
// one card's assignees, and every GraphQL request is a query.
func TestTheOnlyWriteIsTheAssignee(t *testing.T) {
	type call struct {
		method string
		path   string
		query  string
		body   string
	}
	var asked []call
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var decoded struct {
			Query string `json:"query"`
		}
		json.Unmarshal(body, &decoded)
		asked = append(asked, call{method: r.Method, path: r.URL.Path, query: decoded.Query, body: string(body)})
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, emptyBoard)
			return
		}
		fmt.Fprint(w, `{"number":75,"assignees":[{"login":"thani-sh-meme"}]}`)
	})
	if _, err := client.ReadAll(context.Background(), "aivara-se", 2); err != nil {
		t.Fatalf("read all: %v", err)
	}
	if _, err := client.Assignee(context.Background(), "aivara-se", "dispatcher", 75); err != nil {
		t.Fatalf("read card: %v", err)
	}
	if err := client.Assign(context.Background(), "aivara-se", "dispatcher", 75, "thani-sh-meme"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	var writes []call
	for _, ask := range asked {
		switch ask.method {
		case http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodPost:
			if ask.method == http.MethodPost && ask.path == "/graphql" {
				continue
			}
			writes = append(writes, ask)
		}
	}
	if len(writes) != 1 || writes[0].method != http.MethodPatch || writes[0].path != "/repos/aivara-se/dispatcher/issues/75" {
		t.Fatalf("the writes were %+v, want one PATCH of card 75", writes)
	}
	var write map[string][]string
	if err := json.Unmarshal([]byte(writes[0].body), &write); err != nil {
		t.Fatalf("the write body %q does not decode: %v", writes[0].body, err)
	}
	want := map[string][]string{"assignees": {"thani-sh-meme"}}
	if !reflect.DeepEqual(write, want) {
		t.Errorf("the write body was %q, want only the assignee", writes[0].body)
	}
	for _, ask := range asked {
		if ask.path != "/graphql" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(ask.query), "query") || strings.Contains(ask.query, "mutation") {
			t.Errorf("the graphql request was %q, want a read-only query", ask.query)
		}
	}
}
