package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAniList answers the mediaListEntry query with the given entry and
// records any SaveMediaListEntry mutation
func fakeAniList(t *testing.T, progress int, status string) (saved *map[string]any, done func()) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header")
		}
		if strings.Contains(body.Query, "SaveMediaListEntry") {
			got = body.Variables
			fmt.Fprint(w, `{"data":{}}`)
			return
		}
		if status == "" {
			fmt.Fprint(w, `{"data":{"Media":{"mediaListEntry":null}}}`)
			return
		}
		fmt.Fprintf(w, `{"data":{"Media":{"mediaListEntry":{"progress":%d,"status":%q}}}}`, progress, status)
	}))
	old := anilistEndpoint
	anilistEndpoint = srv.URL
	return &got, func() { anilistEndpoint = old; srv.Close() }
}

func TestUpdateAniListProgress(t *testing.T) {
	total := 12
	cases := []struct {
		name           string
		progress       int
		status         string
		episode        int
		wantSkip       bool
		wantSaveStatus string
	}{
		{"next episode", 4, "CURRENT", 5, false, "CURRENT"},
		{"not on list yet", 0, "", 1, false, "CURRENT"},
		{"from planning", 0, "PLANNING", 1, false, "CURRENT"},
		{"rewatching old episode never lowers progress", 10, "CURRENT", 3, true, ""},
		{"rewatch keeps REPEATING", 2, "REPEATING", 3, false, "REPEATING"},
		{"last episode completes", 11, "CURRENT", 12, false, "COMPLETED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			saved, done := fakeAniList(t, c.progress, c.status)
			defer done()
			msg := updateAniListProgress("tok", 42, "Show", c.episode, &total)().(updateProgressMsg)
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			if msg.skipped != c.wantSkip {
				t.Fatalf("skipped = %v", msg.skipped)
			}
			if c.wantSkip {
				if *saved != nil {
					t.Fatal("should not have saved")
				}
				return
			}
			if (*saved)["status"] != c.wantSaveStatus || (*saved)["progress"] != float64(c.episode) {
				t.Fatalf("saved %v", *saved)
			}
		})
	}
}

func TestAniListErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"data":null,"errors":[{"message":"Invalid token"}]}`)
	}))
	defer srv.Close()
	old := anilistEndpoint
	anilistEndpoint = srv.URL
	defer func() { anilistEndpoint = old }()

	if _, err := makePublicRequest(`query { Viewer { id } }`, nil); err == nil || !strings.Contains(err.Error(), "Invalid token") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchMediaPage(t *testing.T) {
	var vars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		vars = body.Variables
		if !strings.Contains(body.Query, "SCORE_DESC") {
			t.Errorf("top rated should sort by score: %s", body.Query)
		}
		fmt.Fprint(w, `{"data":{"Page":{"pageInfo":{"currentPage":3,"lastPage":9,"hasNextPage":true},
			"media":[{"id":1,"title":{"romaji":"A"}},{"id":2,"title":{"romaji":"B"}}]}}}`)
	}))
	defer srv.Close()
	old := anilistEndpoint
	anilistEndpoint = srv.URL
	defer func() { anilistEndpoint = old }()

	msg := fetchMediaPage(ListTopRated, 3)().(listPageMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if vars["page"] != float64(3) || vars["perPage"] != float64(listPerPage) {
		t.Fatalf("vars %v", vars)
	}
	if msg.page != 3 || msg.lastPage != 9 || !msg.hasNext || len(msg.entries) != 2 {
		t.Fatalf("msg %+v", msg)
	}
}
