//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topicposts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reddit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// fakeRedditListing is Reddit's JSON shape for one newest page of r/golang, with fields the connector drops.
const fakeRedditListing = `{"kind":"Listing","data":{"after":"t3_1a0002","dist":2,"modhash":"","children":[` +
	`{"kind":"t3","data":{"name":"t3_1a0001","title":"Weekly questions thread","subreddit":"golang","author":"example_moderator","permalink":"/r/golang/comments/1a0001/weekly_questions_thread/","url":"https://www.reddit.com/r/golang/comments/1a0001/weekly_questions_thread/","is_self":true,"selftext":"Generics questions welcome.","score":12,"num_comments":40,"created_utc":1794211200.0,"over_18":false,"spoiler":false,"stickied":true,"ups":12}},` +
	`{"kind":"t3","data":{"name":"t3_1a0002","title":"Generics and iterators together","subreddit":"golang","author":"example_author_two","permalink":"/r/golang/comments/1a0002/generics_and_iterators_together/","url":"https://example.com/iterators","is_self":false,"selftext":"","score":31,"num_comments":4,"created_utc":1794475700.0,"over_18":false,"spoiler":false,"stickied":false,"ups":31}}` +
	`],"before":null}}`

// fakeRedditSearch is Reddit's JSON shape for one search page that repeats the listing's matching post.
const fakeRedditSearch = `{"kind":"Listing","data":{"after":null,"dist":2,"children":[` +
	`{"kind":"t3","data":{"name":"t3_1a0004","title":"Benchmarking generic containers","subreddit":"golang","author":"example_author_five","permalink":"/r/golang/comments/1a0004/benchmarking_generic_containers/","url":"https://www.reddit.com/r/golang/comments/1a0004/benchmarking_generic_containers/","is_self":true,"selftext":"Results for **maps** & slices.","score":44,"num_comments":15,"created_utc":1794564000.0,"over_18":false,"spoiler":false,"stickied":false}},` +
	`{"kind":"t3","data":{"name":"t3_1a0002","title":"Generics and iterators together","subreddit":"golang","author":"example_author_two","permalink":"/r/golang/comments/1a0002/generics_and_iterators_together/","url":"https://example.com/iterators","is_self":false,"selftext":"","score":31,"num_comments":4,"created_utc":1794475700.0,"over_18":false,"spoiler":false,"stickied":false}}` +
	`],"before":null}}`

func TestTopicPostsReadsAFakeRedditThroughTheRealClientWithRealDex(t *testing.T) {
	var requestsMutex sync.Mutex
	var requests []*url.URL
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMutex.Lock()
		requests = append(requests, r.URL)
		requestsMutex.Unlock()
		if r.Header.Get("Authorization") != "Bearer synthetic-access-token" || r.Header.Get("User-Agent") != "desktop:synthetic-client-id:v0.1.0 (by /u/example_user)" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Ratelimit-Remaining", "99.0")
		// A failed write reaches the Flow as an invalid response, which the assertions below report.
		switch r.URL.Path {
		case "/r/golang/new":
			_, _ = w.Write([]byte(fakeRedditListing))
		case "/r/golang/search":
			_, _ = w.Write([]byte(fakeRedditSearch))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer provider.Close()
	reference := sdkgo.ConnectionRef{Provider: "reddit", Name: ConnectionName}
	credentials := reddit.Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("synthetic-access-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	redditClient, err := reddit.New(reddit.Config{BaseURL: provider.URL, RedditUsername: "example_user"}, sdkgo.StaticCredentialProvider[reddit.Credentials]{reference: credentials})
	require.NoError(t, err)
	connection, err := reddit.NewConnection(redditClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	client := startTopicPostsWorker(t, flow)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "reddit-topic-posts-fake-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, TopicPostsRequest{Subreddit: "golang", Topic: "generics"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output TopicPostsOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, []string{"t3_1a0004", "t3_1a0002"}, postIDs(output.Posts))
	require.Equal(t, "https://www.reddit.com/r/golang/comments/1a0004/benchmarking_generic_containers/", output.Posts[0].Permalink)
	require.Equal(t, "Results for **maps** & slices.", output.Posts[0].SelfText)
	require.Equal(t, time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC), output.Posts[0].CreatedAt)
	require.Equal(t, "https://example.com/iterators", output.Posts[1].LinkURL)
	require.Empty(t, output.SearchAfter)
	requestsMutex.Lock()
	defer requestsMutex.Unlock()
	require.Len(t, requests, 2)
	require.Equal(t, "/r/golang/new", requests[0].Path)
	require.Equal(t, url.Values{"limit": {"25"}, "raw_json": {"1"}}, requests[0].Query())
	require.Equal(t, "/r/golang/search", requests[1].Path)
	require.Equal(t, url.Values{
		"q": {"generics"}, "restrict_sr": {"true"}, "type": {"link"}, "sort": {"new"}, "t": {"week"}, "limit": {"25"}, "raw_json": {"1"},
	}, requests[1].Query())
}
