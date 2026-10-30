// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const envelopesPath = accountPath + "/envelopes"

func validCreateInput() docusign.CreateEnvelopeFromTemplateInput {
	return docusign.CreateEnvelopeFromTemplateInput{
		TemplateID: testTemplateID,
		TemplateRoles: []docusign.TemplateRole{
			{RoleName: "Internal Legal", Name: "Dana Iwu", Email: "dana@example.com", RoutingOrder: 1},
			{RoleName: "Customer", Name: "Priya Raman", Email: "priya@meridian.example.com"},
		},
		EmailSubject: "Meridian Corp: Master Services Agreement",
		CustomFields: []docusign.EnvelopeCustomField{{Name: "dexSigningRequestId", Value: "opp-123"}},
	}
}

func createdEnvelopeJSON(status string) string {
	return `{"envelopeId":"` + testEnvelopeID + `","status":"` + status + `","statusDateTime":"2026-10-04T12:00:00.0000000Z","uri":"/envelopes/` + testEnvelopeID + `"}`
}

// fakeEnvelopeStore stores created envelopes and lists them by the marker custom field, like DocuSign.
type fakeEnvelopeStore struct {
	mu                         sync.Mutex
	envelopes                  map[string]string
	isCustomFieldFilterIgnored bool
}

func (store *fakeEnvelopeStore) create(request *http.Request) string {
	var body struct {
		CustomFields struct {
			TextCustomFields []struct{ Name, Value string } `json:"textCustomFields"`
		} `json:"customFields"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		panic(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.envelopes == nil {
		store.envelopes = map[string]string{}
	}
	envelopeID := testEnvelopeID
	if len(store.envelopes) > 0 {
		envelopeID = otherEnvelopeID
	}
	store.envelopes[envelopeID] = body.CustomFields.TextCustomFields[0].Value
	return envelopeID
}

// list answers GET /envelopes. Like DocuSign, it returns custom fields only for include=custom_fields.
func (store *fakeEnvelopeStore) list(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	marker := strings.TrimPrefix(query.Get("custom_field"), "dexIdempotencyKey=")
	isCustomFieldIncluded := slices.Contains(strings.Split(query.Get("include"), ","), "custom_fields")
	store.mu.Lock()
	defer store.mu.Unlock()
	listed := []string{}
	for envelopeID, storedMarker := range store.envelopes {
		if !store.isCustomFieldFilterIgnored && storedMarker != marker {
			continue
		}
		if isCustomFieldIncluded {
			listed = append(listed, envelopeJSON(envelopeID, "sent", storedMarker))
		} else {
			listed = append(listed, `{"envelopeId":"`+envelopeID+`","status":"sent","statusChangedDateTime":"2026-10-04T11:00:00.0000000Z"}`)
		}
	}
	writeJSON(response, http.StatusOK, `{"resultSetSize":"`+string(rune('0'+len(listed)))+`","envelopes":[`+strings.Join(listed, ",")+`]}`)
}

func (store *fakeEnvelopeStore) count() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.envelopes)
}

func TestCreateEnvelopeSendsTheTemplateWithItsRoutingOrderAndMarker(t *testing.T) {
	context := newTestDexContext("create")
	var heartbeatAtDispatch string
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"POST " + envelopesPath: func(response http.ResponseWriter, _ *http.Request) {
		heartbeatAtDispatch = string(context.lastHeartbeat())
		writeJSON(response, http.StatusCreated, createdEnvelopeJSON("sent"))
	}})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})

	result, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Contains(t, heartbeatAtDispatch, `"isDispatched":true`, "the checkpoint precedes the request")
	require.Empty(t, fake.requestsTo(http.MethodGet, envelopesPath), "a first attempt sends without reading back")
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
	require.Equal(t, docusign.CreatedEnvelope{
		EnvelopeID: testEnvelopeID, Status: docusign.EnvelopeStatusSent, StatusChangedAt: &[]time.Time{fixedNow}[0],
	}, result.Value)
	require.Equal(t, testEnvelopeID, result.Receipt.ProviderObjectID)

	posts := fake.requestsTo(http.MethodPost, envelopesPath)
	require.Len(t, posts, 1)
	require.Equal(t, productionBaseHost, posts[0].host)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(posts[0].body), &body))
	require.Equal(t, map[string]any{
		"templateId": testTemplateID, "status": "sent", "emailSubject": "Meridian Corp: Master Services Agreement",
		"templateRoles": []any{
			map[string]any{"roleName": "Internal Legal", "name": "Dana Iwu", "email": "dana@example.com", "routingOrder": "1"},
			map[string]any{"roleName": "Customer", "name": "Priya Raman", "email": "priya@meridian.example.com"},
		},
		"customFields": map[string]any{"textCustomFields": []any{
			map[string]any{"name": "dexIdempotencyKey", "value": string(result.Receipt.IdempotencyKey), "show": "false", "required": "false"},
			map[string]any{"name": "dexSigningRequestId", "value": "opp-123", "show": "false", "required": "false"},
		}},
	}, body)
	requireNoSecrets(t, result)
}

func TestCreateEnvelopeKeepsADraftUnsent(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"POST " + envelopesPath: respondJSON(http.StatusCreated, createdEnvelopeJSON("created"))})
	input := validCreateInput()
	input.IsDraft = true
	result, err := sdkgo.RunMutation(newTestDexContext("draft"), newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, docusign.EnvelopeStatusCreated, result.Value.Status)
	require.Contains(t, fake.requestsTo(http.MethodPost, envelopesPath)[0].body, `"status":"created"`)
}

func TestCreateEnvelopeRejectsInvalidInputWithoutARequest(t *testing.T) {
	for name, mutate := range map[string]func(*docusign.CreateEnvelopeFromTemplateInput){
		"template ID": func(input *docusign.CreateEnvelopeFromTemplateInput) { input.TemplateID = "tpl_msa_v4" },
		"no roles":    func(input *docusign.CreateEnvelopeFromTemplateInput) { input.TemplateRoles = nil },
		"role filled twice": func(input *docusign.CreateEnvelopeFromTemplateInput) {
			input.TemplateRoles[1].RoleName = "internal legal"
		},
		"email":         func(input *docusign.CreateEnvelopeFromTemplateInput) { input.TemplateRoles[0].Email = "dana@example" },
		"blank name":    func(input *docusign.CreateEnvelopeFromTemplateInput) { input.TemplateRoles[0].Name = " " },
		"routing order": func(input *docusign.CreateEnvelopeFromTemplateInput) { input.TemplateRoles[0].RoutingOrder = 1000 },
		"subject":       func(input *docusign.CreateEnvelopeFromTemplateInput) { input.EmailSubject = strings.Repeat("s", 101) },
		"reserved custom field": func(input *docusign.CreateEnvelopeFromTemplateInput) {
			input.CustomFields[0].Name = "DexIdempotencyKey"
		},
		"long custom field": func(input *docusign.CreateEnvelopeFromTemplateInput) {
			input.CustomFields[0].Value = strings.Repeat("v", 101)
		},
		"repeated custom fields": func(input *docusign.CreateEnvelopeFromTemplateInput) {
			input.CustomFields = append(input.CustomFields, input.CustomFields[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, nil)
			input := validCreateInput()
			mutate(&input)
			result, err := sdkgo.RunMutation(newTestDexContext("invalid"), newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, input)
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchDefect, result.Branch)
			require.Empty(t, fake.recordedRequests())
		})
	}
}

func TestCreateEnvelopeRetriesOnlyWhenDocuSignProvablyCreatedNothing(t *testing.T) {
	context := newTestDexContext("rate-limited")
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"POST " + envelopesPath: respondJSON(http.StatusTooManyRequests, docusignError("BURST_APIINVOCATION_LIMIT_EXCEEDED"))})
	_, err := sdkgo.RunMutation(context, newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	require.Nil(t, context.lastHeartbeat(), "a refused create clears its checkpoint")
}

func TestCreateEnvelopeConclusiveRejectionsCreateNothing(t *testing.T) {
	for name, test := range map[string]struct {
		status       int
		body         string
		expectedKind sdkgo.FailureKind
	}{
		"unknown template":     {http.StatusBadRequest, docusignError("TEMPLATE_ID_INVALID"), sdkgo.FailureProviderRejection},
		"no allowance left":    {http.StatusBadRequest, docusignError("ENVELOPE_ALLOWANCE_EXCEEDED"), sdkgo.FailureQuotaExhausted},
		"user may not send":    {http.StatusBadRequest, docusignError("USER_LACKS_PERMISSIONS"), sdkgo.FailureAuthorization},
		"rejected access code": {http.StatusUnauthorized, docusignError("USER_AUTHENTICATION_FAILED"), sdkgo.FailureAuthentication},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"POST " + envelopesPath: respondJSON(test.status, test.body)})
			result, err := sdkgo.RunMutation(newTestDexContext("rejected"), newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchProviderRejected, result.Branch)
			require.Equal(t, test.expectedKind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, "(", "the errorCode names the cause")
			require.Empty(t, fake.requestsTo(http.MethodGet, envelopesPath), "a conclusive rejection needs no read-back")
			requireNoSecrets(t, result)
		})
	}
}

func TestCreateEnvelopeFindsTheEnvelopeAfterAnAmbiguousAnswer(t *testing.T) {
	for name, answer := range map[string]http.HandlerFunc{
		"server error":         respondJSON(http.StatusInternalServerError, docusignError("UNSPECIFIED_ERROR")),
		"unreadable summary":   respondJSON(http.StatusCreated, `{"envelopeId":`),
		"request timeout":      respondJSON(http.StatusRequestTimeout, `{}`),
		"redirect":             respondJSON(http.StatusFound, `{}`),
		"wrong status in body": respondJSON(http.StatusCreated, createdEnvelopeJSON("created")),
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeEnvelopeStore{}
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
					store.create(request)
					answer(response, request)
				},
				"GET " + envelopesPath: store.list,
			})
			result, err := sdkgo.RunMutation(newTestDexContext("ambiguous"), newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
			require.True(t, result.Value.WasRecovered)
			require.Equal(t, testEnvelopeID, result.Value.EnvelopeID)
			reads := fake.requestsTo(http.MethodGet, envelopesPath)
			require.Len(t, reads, 1)
			require.Equal(t, "dexIdempotencyKey="+string(result.Receipt.IdempotencyKey), reads[0].query.Get("custom_field"))
			require.Equal(t, "custom_fields", reads[0].query.Get("include"), "DocuSign lists custom fields only on request")
			require.Equal(t, "2026-10-04T11:00:00Z", reads[0].query.Get("from_date"), "the read-back starts an hour before the checkpoint")
			require.Equal(t, 1, store.count())
		})
	}
}

func TestCreateEnvelopeReportsUncertainWhenTheReadBackCannotProveOneEnvelope(t *testing.T) {
	for name, test := range map[string]struct {
		isCreated                  bool
		isCustomFieldFilterIgnored bool
		listAnswer                 http.HandlerFunc
	}{
		"not listed yet":             {isCreated: false},
		"filter ignored by DocuSign": {isCreated: true, isCustomFieldFilterIgnored: true},
		"listing rejected":           {isCreated: true, listAnswer: respondJSON(http.StatusBadRequest, docusignError("INVALID_REQUEST_PARAMETER"))},
		"listing malformed":          {isCreated: true, listAnswer: respondJSON(http.StatusOK, `{"envelopes":[{"envelopeId":"x"}]}`)},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeEnvelopeStore{isCustomFieldFilterIgnored: test.isCustomFieldFilterIgnored}
			store.envelopes = map[string]string{otherEnvelopeID: "another-step-execution"}
			listAnswer := test.listAnswer
			if listAnswer == nil {
				listAnswer = store.list
			}
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
					if test.isCreated {
						store.mu.Lock()
						store.envelopes[testEnvelopeID] = "unknown-until-decoded"
						store.mu.Unlock()
					}
					writeJSON(response, http.StatusBadGateway, `{}`)
				},
				"GET " + envelopesPath: listAnswer,
			})
			result, err := sdkgo.RunMutation(newTestDexContext("uncertain"), newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch)
			require.Empty(t, result.Value.EnvelopeID)
			require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
		})
	}
}

func TestCreateEnvelopeNeverResendsAfterAnEarlierAttemptDispatched(t *testing.T) {
	store := &fakeEnvelopeStore{}
	var context *testDexContext
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopesPath: store.list})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})

	context = newTestDexContext("retried")
	require.NoError(t, context.RecordHeartbeat(map[string]any{"isDispatched": true, "dispatchedAt": fixedNow.Add(-time.Minute)}))
	result, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "nothing listed yet")
	require.Empty(t, fake.requestsTo(http.MethodPost, envelopesPath))

	store.envelopes = map[string]string{testEnvelopeID: string(result.Receipt.IdempotencyKey)}
	result, err = sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
	require.True(t, result.Value.WasRecovered)
	require.Empty(t, fake.requestsTo(http.MethodPost, envelopesPath))
}

func TestCreateEnvelopeRetriesAReadBackThatDocuSignRateLimitedAndKeepsTheCheckpoint(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopesPath: respondJSON(http.StatusTooManyRequests, docusignError("BURST_APIINVOCATION_LIMIT_EXCEEDED"))})
	context := newTestDexContext("throttled")
	require.NoError(t, context.RecordHeartbeat(map[string]any{"isDispatched": true, "dispatchedAt": fixedNow}))
	_, err := sdkgo.RunMutation(context, newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	var retry *sdkgo.RetryError
	require.True(t, errors.As(err, &retry))
	require.Contains(t, string(context.lastHeartbeat()), `"isDispatched":true`)
	require.Empty(t, fake.requestsTo(http.MethodPost, envelopesPath))
}

func TestCreateEnvelopeReportsUncertainWhenALaterAttemptCannotOpenItsSession(t *testing.T) {
	for name, test := range map[string]struct {
		config      docusign.Config
		routes      map[string]http.HandlerFunc
		credentials func() docusign.CredentialSource
	}{
		"userinfo rejects the token": {routes: map[string]http.HandlerFunc{"GET /oauth/userinfo": respondJSON(http.StatusUnauthorized, `{"error":"invalid_token"}`)}},
		"userinfo forbids the user":  {routes: map[string]http.HandlerFunc{"GET /oauth/userinfo": respondJSON(http.StatusForbidden, `{}`)}},
		"host outside the environment": {routes: map[string]http.HandlerFunc{
			"GET /oauth/userinfo": respondJSON(http.StatusOK, userInfoJSON(testAccountID, "https://na3.docusign.example")),
		}},
		"account the user left": {config: docusign.Config{AccountID: "a4ec37d6-7777-8888-9999-143885c444bb"}},
		"refresh token revoked": {
			routes: map[string]http.HandlerFunc{"POST /oauth/token": respondJSON(http.StatusBadRequest, `{"error":"invalid_grant"}`)},
			credentials: func() docusign.CredentialSource {
				return testsupport.NewRefreshingCredentialSource(productionCredentials(), nil)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, test.routes)
			var credentials docusign.CredentialSource = staticCredentials(productionCredentials())
			if test.credentials != nil {
				credentials = test.credentials()
			}
			context := newRetriedTestDexContext("dispatched-then-unauthorized", 2)
			require.NoError(t, context.RecordHeartbeat(map[string]any{"isDispatched": true, "dispatchedAt": fixedNow.Add(-time.Minute)}))
			result, err := sdkgo.RunMutation(context, newTestClient(t, fake, credentials, test.config).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "the earlier attempt may have created the envelope")
			require.Contains(t, result.Failure.Message, "an earlier attempt sent the DocuSign create request")
			require.Empty(t, fake.requestsTo(http.MethodPost, envelopesPath))
			require.Contains(t, string(context.lastHeartbeat()), `"isDispatched":true`, "the checkpoint survives for a later attempt")
			requireNoSecrets(t, result)
		})
	}
}

func TestCreateEnvelopeLaterAttemptWithoutACheckpointReadsBackBeforeSending(t *testing.T) {
	store := &fakeEnvelopeStore{}
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
			envelopeID := store.create(request)
			writeJSON(response, http.StatusCreated, strings.ReplaceAll(createdEnvelopeJSON("sent"), testEnvelopeID, envelopeID))
		},
		"GET " + envelopesPath: store.list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})

	result, err := sdkgo.RunMutation(newRetriedTestDexContext("lost-checkpoint", 2), client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
	require.False(t, result.Value.WasRecovered, "nothing was listed, so this attempt sent the envelope")
	reads := fake.requestsTo(http.MethodGet, envelopesPath)
	require.Len(t, reads, 1)
	require.Equal(t, "dexIdempotencyKey="+string(result.Receipt.IdempotencyKey), reads[0].query.Get("custom_field"))
	require.Equal(t, "custom_fields", reads[0].query.Get("include"))
	require.Equal(t, "2026-10-04T11:00:00Z", reads[0].query.Get("from_date"), "the read-back starts an hour before the first attempt")
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)

	// The attempt after a Worker lost before Dex stored its checkpoint.
	result, err = sdkgo.RunMutation(newRetriedTestDexContext("lost-checkpoint", 3), client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
	require.True(t, result.Value.WasRecovered)
	require.Equal(t, testEnvelopeID, result.Value.EnvelopeID)
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1, "the tagged envelope is adopted, not sent again")
	require.Equal(t, 1, store.count())
}

func TestCreateEnvelopeLaterAttemptSendsNothingWhenSeveralEnvelopesCarryItsMarker(t *testing.T) {
	store := &fakeEnvelopeStore{}
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
			store.create(request)
			writeJSON(response, http.StatusCreated, createdEnvelopeJSON("sent"))
		},
		"GET " + envelopesPath: store.list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	first, err := sdkgo.RunMutation(newTestDexContext("tagged-twice"), client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	store.mu.Lock()
	store.envelopes[otherEnvelopeID] = string(first.Receipt.IdempotencyKey)
	store.mu.Unlock()

	result, err := sdkgo.RunMutation(newRetriedTestDexContext("tagged-twice", 2), client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1, "only the first attempt sent")
}
