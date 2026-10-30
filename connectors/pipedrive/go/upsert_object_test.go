// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// fakePersonStore answers persons requests; like Pipedrive, its exact search ignores case and returns look-alike addresses.
type fakePersonStore struct {
	mutex            sync.Mutex
	persons          map[string]map[string]any
	nextID           int
	failsNextCreate  bool
	searchExtraEmail string
}

func newFakePersonStore() *fakePersonStore {
	return &fakePersonStore{persons: map[string]map[string]any{}, nextID: 900}
}

func (store *fakePersonStore) add(name string, email string) string {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.nextID++
	id := fmt.Sprint(store.nextID)
	store.persons[id] = map[string]any{"id": store.nextID, "name": name, "emails": []map[string]any{{"value": email, "primary": true}}}
	return id
}

func (store *fakePersonStore) serve(t *testing.T) func(http.ResponseWriter, recordedRequest) {
	return func(response http.ResponseWriter, request recordedRequest) {
		store.mutex.Lock()
		defer store.mutex.Unlock()
		switch {
		case request.method == http.MethodGet && request.path == "/api/v2/persons/search":
			term := strings.ToLower(request.query["term"][0])
			items := []map[string]any{}
			for _, person := range store.persons {
				email := person["emails"].([]map[string]any)[0]["value"].(string)
				if strings.ToLower(email) == term || (store.searchExtraEmail != "" && email == store.searchExtraEmail) {
					items = append(items, map[string]any{"result_score": 1, "item": map[string]any{
						"id": person["id"], "type": "person", "name": person["name"], "emails": []string{email},
					}})
				}
			}
			encoded, err := json.Marshal(map[string]any{"success": true, "data": map[string]any{"items": items}, "additional_data": map[string]any{"next_cursor": nil}})
			require.NoError(t, err)
			writeJSON(t, response, http.StatusOK, string(encoded))
		case request.method == http.MethodPost && request.path == "/api/v2/persons":
			var body map[string]any
			require.NoError(t, json.Unmarshal(request.body, &body))
			store.nextID++
			body["id"] = store.nextID
			emails := body["emails"].([]any)
			body["emails"] = []map[string]any{{"value": emails[0].(map[string]any)["value"], "primary": true}}
			store.persons[fmt.Sprint(store.nextID)] = body
			if store.failsNextCreate {
				store.failsNextCreate = false
				writeError(t, response, http.StatusServiceUnavailable)
				return
			}
			store.writePerson(t, response, body)
		case request.method == http.MethodPatch && strings.HasPrefix(request.path, "/api/v2/persons/"):
			person := store.persons[strings.TrimPrefix(request.path, "/api/v2/persons/")]
			var body map[string]any
			require.NoError(t, json.Unmarshal(request.body, &body))
			for name, value := range body {
				person[name] = value
			}
			store.writePerson(t, response, person)
		default:
			writeError(t, response, http.StatusNotFound)
		}
	}
}

func (store *fakePersonStore) writePerson(t *testing.T, response http.ResponseWriter, person map[string]any) {
	encoded, err := json.Marshal(person)
	require.NoError(t, err)
	writeRecord(t, response, string(encoded))
}

func (store *fakePersonStore) count() int {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return len(store.persons)
}

func personUpsert(email string) pipedrive.UpsertObjectInput {
	return pipedrive.UpsertObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, IDProperty: pipedrive.IdentityPropertyEmail, IDValue: email,
		Fields: map[string]json.RawMessage{"name": pipedrive.StringValue("Jane Smith"), "owner_id": pipedrive.IDValue("7")},
	}
}

func TestUpsertObjectCreatesAPersonWithThePrimaryEmailWhenNoneMatches(t *testing.T) {
	store := newFakePersonStore()
	store.add("Jane Smith-Okafor", "jane.smith@acme.example.com")
	store.searchExtraEmail = "jane.smith@acme.example.com"
	provider := newRecordingPipedrive(t, store.serve(t))
	result, err := sdkgo.RunMutation(newStepDexContext("upsert-new"), newAPITokenClient(t, provider.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
	require.True(t, result.Value.Created)
	require.Equal(t, []string{"jane@acme.example.com"}, result.Value.Object.Emails)
	requests := provider.recordedRequests()
	require.Len(t, requests, 2)
	require.Equal(t, map[string][]string{
		"term": {"jane@acme.example.com"}, "fields": {"email"}, "exact_match": {"true"}, "limit": {"50"},
	}, requests[0].query)
	require.JSONEq(t, `{"name":"Jane Smith","owner_id":7,"emails":[{"value":"jane@acme.example.com","primary":true,"label":"work"}]}`, string(requests[1].body))
	require.Equal(t, 2, store.count(), "the look-alike decoy address is not a match")
}

func TestUpsertObjectUpdatesTheOneMatchIgnoringCase(t *testing.T) {
	store := newFakePersonStore()
	existing := store.add("J. Smith", "Jane@Acme.example.com")
	provider := newRecordingPipedrive(t, store.serve(t))
	result, err := sdkgo.RunMutation(newStepDexContext("upsert-existing"), newAPITokenClient(t, provider.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
	require.False(t, result.Value.Created)
	require.Equal(t, existing, result.Value.Object.ID)
	require.Equal(t, "Jane Smith", result.Value.Object.Name)
	requests := provider.recordedRequests()
	require.Equal(t, http.MethodPatch, requests[1].method)
	require.JSONEq(t, `{"name":"Jane Smith","owner_id":7}`, string(requests[1].body), "an update never rewrites the emails")
	require.Equal(t, 1, store.count())
}

func TestUpsertObjectWritesNothingWhenSeveralPersonsShareTheEmail(t *testing.T) {
	store := newFakePersonStore()
	first, second := store.add("Jane", "jane@acme.example.com"), store.add("Jane S", "jane@acme.example.com")
	provider := newRecordingPipedrive(t, store.serve(t))
	result, err := sdkgo.RunMutation(newStepDexContext("upsert-duplicates"), newAPITokenClient(t, provider.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchMultipleMatches, result.Branch)
	require.ElementsMatch(t, []string{first, second}, result.Value.MatchingIDs)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Len(t, provider.recordedRequests(), 1, "only the search was sent")
}

func TestUpsertObjectReadsBackAnUnconfirmedCreateInsteadOfCreatingAgain(t *testing.T) {
	store := newFakePersonStore()
	store.failsNextCreate = true
	provider := newRecordingPipedrive(t, store.serve(t))
	client := newAPITokenClient(t, provider.URL)
	ctx := newStepDexContext("upsert-lost-create")
	_, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "an unconfirmed create is retried")
	require.True(t, ctx.hasHeartbeat(), "the checkpoint stays so the retry reads the person back")

	ctx.nextAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
	require.False(t, result.Value.Created, "the retry found the person the first attempt created")
	require.Equal(t, 1, store.count())
	methods := []string{}
	for _, request := range provider.recordedRequests() {
		methods = append(methods, request.method+" "+request.path)
	}
	require.Equal(t, []string{"GET /api/v2/persons/search", "POST /api/v2/persons", "GET /api/v2/persons/search", "PATCH /api/v2/persons/901"}, methods)
}

func TestUpsertObjectCreateRetries503WithSharedFence(t *testing.T) {
	createRequests := 0
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
			return
		}
		createRequests++
		if createRequests == 1 {
			writeError(t, response, http.StatusServiceUnavailable)
			return
		}
		writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
	})
	client := newAPITokenClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("deal-intake", "upsert-503")
	_, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
	require.True(t, result.Value.Created)
	require.Equal(t, 2, createRequests)
	require.Len(t, provider.recordedRequests(), 4)
}

func TestUpsertObjectFailsClosedForALegacyCheckpoint(t *testing.T) {
	store := newFakePersonStore()
	provider := newRecordingPipedrive(t, store.serve(t))
	ctx := newStepDexContext("upsert-unfound")
	require.NoError(t, ctx.RecordHeartbeat(map[string]string{"pipedriveDispatchedCallId": "earlier"}))
	result, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, provider.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Zero(t, store.count(), "the connector never creates a second time")
	require.Empty(t, provider.recordedRequests(), "a legacy checkpoint cannot authorize a read or send")
}

func TestUpsertObjectSearchesAfterAClaimAndSelectsUncertainWhenCreateCannotBeFound(t *testing.T) {
	seed := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
			return
		}
		writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
	})
	ctx := newStepDexContext("upsert-claimed-search-miss")
	seedResult, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, seed.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.NoError(t, ctx.RecordHeartbeat(singledispatch.Marker{CallID: seedResult.Receipt.CallID, Attempt: 1, State: singledispatch.StateClaimed}))
	ctx.nextAttempt()
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
	})
	result, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, provider.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
	require.Equal(t, http.MethodGet, provider.recordedRequests()[0].method)
}

func TestUpsertObjectReadsSavedProviderIDWithoutAnotherCreate(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.path == "/api/v2/persons/search" {
			writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
			return
		}
		writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
	})
	ctx := singledispatchtest.NewContext("deal-intake", "upsert-saved-id")
	client := newAPITokenClient(t, provider.URL)
	first, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.True(t, first.Value.Created)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, second.Branch)
	require.False(t, second.Value.Created)
	var methods []string
	for _, request := range provider.recordedRequests() {
		methods = append(methods, request.method+" "+request.path)
	}
	require.Equal(t, []string{"GET /api/v2/persons/search", "POST /api/v2/persons", "GET /api/v2/persons/901", "PATCH /api/v2/persons/901"}, methods)
}

func TestUpsertObjectRecordsTransientCredentialFailureBeforeSearching(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
			return
		}
		writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
	})
	expired := time.Now().Add(-time.Minute)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expired)
	refreshes := 0
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		refreshes++
		if refreshes == 1 {
			writeError(t, response, http.StatusServiceUnavailable)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"access_token":"refreshed-access-token","token_type":"Bearer","expires_in":3599,"api_domain":"`+provider.URL+`"}`)
	}}}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("deal-intake", "upsert-credential-retry")
	_, err = sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, provider.recordedRequests())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
	require.Equal(t, 2, refreshes)
	require.Len(t, provider.recordedRequests(), 2)
}

func TestUpsertObjectRecordsNotSentBeforeSearchAndUpdateRetries(t *testing.T) {
	for _, failingPath := range []string{"search", "update"} {
		t.Run(failingPath, func(t *testing.T) {
			attempt := 0
			provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
				if request.method == http.MethodGet && failingPath == "search" && attempt == 0 {
					writeError(t, response, http.StatusServiceUnavailable)
					return
				}
				if request.method == http.MethodGet {
					if failingPath == "search" {
						writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
					} else {
						writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[{"item":{"id":901,"type":"person","name":"Jane","emails":["jane@acme.example.com"]}}]},"additional_data":{"next_cursor":null}}`)
					}
					return
				}
				if failingPath == "update" && attempt == 0 {
					writeError(t, response, http.StatusServiceUnavailable)
					return
				}
				if failingPath == "update" {
					writeError(t, response, http.StatusForbidden)
				} else {
					writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
				}
			})
			ctx := singledispatchtest.NewContext("deal-intake", "not-sent-"+failingPath)
			client := newAPITokenClient(t, provider.URL)
			_, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			ctx.AdvanceAttempt()
			attempt++
			result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
			require.NoError(t, err)
			if failingPath == "update" {
				require.Equal(t, pipedrive.UpsertObjectBranchProviderRejected, result.Branch)
			} else {
				require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
			}
		})
	}
}

func TestUpsertObjectMayCreateAfterAnEarlierPatchRetry(t *testing.T) {
	searches := 0
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		switch request.method {
		case http.MethodGet:
			searches++
			if searches == 1 {
				writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[{"item":{"id":901,"type":"person","name":"Jane","emails":["jane@acme.example.com"]}}]},"additional_data":{"next_cursor":null}}`)
				return
			}
			writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
		case http.MethodPatch:
			writeError(t, response, http.StatusServiceUnavailable)
		case http.MethodPost:
			writeRecord(t, response, `{"id":902,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
		}
	})
	ctx := singledispatchtest.NewContext("deal-intake", "patch-then-missing")
	client := newAPITokenClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
	require.True(t, result.Value.Created)
	var posts int
	for _, request := range provider.recordedRequests() {
		if request.method == http.MethodPost {
			posts++
		}
	}
	require.Equal(t, 1, posts)
}

func TestUpsertObjectReadsBackAnAcceptedCreateWithAnUnusableBody(t *testing.T) {
	for _, readBack := range []string{"found", "missing", "transient"} {
		t.Run(readBack, func(t *testing.T) {
			searches := 0
			provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
				switch request.path {
				case "/api/v2/persons/search":
					searches++
					if searches == 1 || readBack == "missing" {
						writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
						return
					}
					if readBack == "transient" {
						writeError(t, response, http.StatusServiceUnavailable)
						return
					}
					writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[{"item":{"id":901,"type":"person","name":"Jane","emails":["jane@acme.example.com"]}}]},"additional_data":{"next_cursor":null}}`)
				case "/api/v2/persons":
					writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"id":"unusable"}}`)
				case "/api/v2/persons/901":
					writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
				}
			})
			ctx := singledispatchtest.NewContext("deal-intake", "accepted-unusable-"+readBack)
			result, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, provider.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
			if readBack == "transient" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			} else {
				require.NoError(t, err)
				if readBack == "found" {
					require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
					require.Equal(t, "901", result.Value.Object.ID)
				} else {
					require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
				}
			}
			var posts int
			for _, request := range provider.recordedRequests() {
				if request.method == http.MethodPost {
					posts++
				}
			}
			require.Equal(t, 1, posts)
			require.Equal(t, 2, searches, "read-back stays in the create attempt")
		})
	}
}

// After an unconfirmed create, providerRejected or defect would wrongly report that nothing was created.
func TestUpsertObjectSelectsUncertainWhenAnAttemptAfterAnUnconfirmedCreateIsRefused(t *testing.T) {
	refusedSearch := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeError(t, response, http.StatusForbidden)
	})
	refusedUpdate := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[{"item":{"id":901,"type":"person","name":"Jane","emails":["jane@acme.example.com"]}}]},"additional_data":{"next_cursor":null}}`)
			return
		}
		writeError(t, response, http.StatusForbidden)
	})
	withoutCredentials, err := pipedrive.New(pipedrive.Config{Endpoint: refusedSearch.URL}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{})
	require.NoError(t, err)
	for name, testCase := range map[string]struct {
		client       *pipedrive.Client
		failureKind  sdkgo.FailureKind
		requestCount int
	}{
		"search refused":          {newAPITokenClient(t, refusedSearch.URL), sdkgo.FailureAuthorization, 1},
		"update refused":          {newAPITokenClient(t, refusedUpdate.URL), sdkgo.FailureAuthorization, 2},
		"credentials unavailable": {withoutCredentials, sdkgo.FailureAuthentication, 0},
	} {
		before := len(refusedSearch.recordedRequests()) + len(refusedUpdate.recordedRequests())
		ctx := newStepDexContext("upsert-refused-after-create")
		seed := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
			if request.method == http.MethodGet {
				writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
				return
			}
			writeRecord(t, response, `{"id":901,"name":"Jane","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
		})
		seedResult, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, seed.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
		require.NoError(t, err)
		require.NoError(t, ctx.RecordHeartbeat(singledispatch.Marker{CallID: seedResult.Receipt.CallID, Attempt: 1, State: singledispatch.StateClaimed}))
		ctx.nextAttempt()
		result, err := sdkgo.RunMutation(ctx, testCase.client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
		require.NoError(t, err, name)
		require.Equal(t, sdkgo.UncertainBranchID, result.Branch, name)
		require.Equal(t, testCase.failureKind, result.Failure.Kind, name)
		require.NotContains(t, result.Failure.Message, providerMessageSentinel, name)
		after := len(refusedSearch.recordedRequests()) + len(refusedUpdate.recordedRequests())
		require.Equal(t, testCase.requestCount, after-before, "%s sent no create", name)
	}

	result, err := sdkgo.RunMutation(newStepDexContext("upsert-refused-first"), newAPITokenClient(t, refusedSearch.URL).UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchProviderRejected, result.Branch, "without an earlier create nothing was created")
}

func TestUpsertObjectCreatesAnOrganizationNamedByTheIdentityValue(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[{"item":{"id":5,"type":"organization","name":"Acme Corp (EU)"}}]},"additional_data":{"next_cursor":null}}`)
			return
		}
		writeRecord(t, response, `{"id":88,"name":"Acme Corp","owner_id":7}`)
	})
	result, err := sdkgo.RunMutation(newStepDexContext("upsert-org"), newAPITokenClient(t, provider.URL).UpsertObject(), pipedriveConnection, pipedrive.UpsertObjectInput{
		ObjectType: pipedrive.ObjectTypeOrganizations, IDProperty: pipedrive.IdentityPropertyName, IDValue: "Acme Corp",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpsertObjectBranchUpserted, result.Branch)
	require.True(t, result.Value.Created, "a different organization name is not a match")
	requests := provider.recordedRequests()
	require.Equal(t, "name", requests[0].query["fields"][0])
	require.JSONEq(t, `{"name":"Acme Corp"}`, string(requests[1].body))
}

func TestUpsertObjectRejectsInputWithoutAnIdentityItCanSearch(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeError(t, response, http.StatusInternalServerError)
	})
	client := newAPITokenClient(t, provider.URL)
	withEmails := personUpsert("jane@acme.example.com")
	withEmails.Fields["emails"] = json.RawMessage(`[]`)
	withoutName := personUpsert("jane@acme.example.com")
	delete(withoutName.Fields, "name")
	for name, input := range map[string]pipedrive.UpsertObjectInput{
		"deals have no identity":          {ObjectType: pipedrive.ObjectTypeDeals, IDProperty: "title", IDValue: "Acme"},
		"person by name":                  {ObjectType: pipedrive.ObjectTypePersons, IDProperty: "name", IDValue: "Jane"},
		"display-name address":            personUpsert("Jane <jane@acme.example.com>"),
		"emails set by the caller":        withEmails,
		"person without a name":           withoutName,
		"organization name with spaces":   {ObjectType: pipedrive.ObjectTypeOrganizations, IDProperty: "name", IDValue: " Acme"},
		"organization name in the fields": {ObjectType: pipedrive.ObjectTypeOrganizations, IDProperty: "name", IDValue: "Acme", Fields: map[string]json.RawMessage{"name": pipedrive.StringValue("B")}},
	} {
		result, err := sdkgo.RunMutation(newStepDexContext("upsert-invalid"), client.UpsertObject(), pipedriveConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, pipedrive.UpsertObjectBranchDefect, result.Branch, name)
	}
	require.Empty(t, provider.recordedRequests())
}
