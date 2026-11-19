// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/go"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
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
	require.Contains(t, string(context.lastHeartbeat()), `"isDispatched":false`, "a refused create counts no send")
	require.Contains(t, string(context.lastHeartbeat()), `"sendCount":0`)
}

func TestCreateEnvelopeServiceUnavailableRetriesWithoutCountingASend(t *testing.T) {
	context := newTestDexContext("unavailable")
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"POST " + envelopesPath: respondJSON(http.StatusServiceUnavailable, docusignError("UNSPECIFIED_ERROR"))})
	_, err := sdkgo.RunMutation(context, newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, fake.requestsTo(http.MethodGet, envelopesPath), "DocuSign answers 503 before it processes the create")
	require.Contains(t, string(context.lastHeartbeat()), `"isDispatched":false`)
}

// runCreateAttempt runs one attempt of a Step execution from the heartbeat the previous attempt left.
func runCreateAttempt(t *testing.T, client *docusign.Client, heartbeat json.RawMessage, attempt int32) (sdkgo.MutationResult[docusign.CreatedEnvelope], json.RawMessage, error) {
	t.Helper()
	context := newRetriedTestDexContext("sequenced", attempt)
	context.heartbeatValue = heartbeat
	result, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	return result, context.lastHeartbeat(), err
}

func TestCreateEnvelopeDoesNotCountRateLimitedResendsAndReadsBackBeforeEachResend(t *testing.T) {
	store := &fakeEnvelopeStore{}
	var posts int
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
			posts++
			switch posts {
			case 1:
				writeJSON(response, http.StatusInternalServerError, docusignError("UNSPECIFIED_ERROR"))
			case 2, 3:
				writeJSON(response, http.StatusTooManyRequests, docusignError("BURST_APIINVOCATION_LIMIT_EXCEEDED"))
			default:
				envelopeID := store.create(request)
				writeJSON(response, http.StatusCreated, strings.ReplaceAll(createdEnvelopeJSON("sent"), testEnvelopeID, envelopeID))
			}
		},
		"GET " + envelopesPath: store.list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	var heartbeat json.RawMessage
	for attempt := int32(1); attempt <= 3; attempt++ {
		var err error
		_, heartbeat, err = runCreateAttempt(t, client, heartbeat, attempt)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d", attempt)
		require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath), int(attempt), "every attempt after the unknown send reads back first")
	}
	require.Contains(t, string(heartbeat), `"sendCount":1`, "the rate-limited resends are not counted")
	result, _, err := runCreateAttempt(t, client, heartbeat, 4)
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch, "one counted send leaves room below the cap of three")
	require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath), 4, "the fourth attempt read back before it resent")
	require.Equal(t, 4, posts)
}

func TestCreateEnvelopeNeverResendsAnAcceptedCreate(t *testing.T) {
	for name, answer := range map[string]http.HandlerFunc{
		"unusable summary": respondJSON(http.StatusCreated, `{"envelopeId":`),
		"body cut off after the headers": func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Content-Length", "200")
			response.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(response, `{"envelopeId":`) // The short body breaks the client's read.
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"POST " + envelopesPath: answer,
				"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
			})
			client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
			_, heartbeat, err := runCreateAttempt(t, client, nil, 1)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the listing may lag behind the accepted create")
			require.Contains(t, string(heartbeat), `"isAccepted":true`)

			_, heartbeat, err = runCreateAttempt(t, client, heartbeat, 2)
			require.ErrorAs(t, err, &retry, "a later attempt only reads back")
			result, _, err := runCreateAttempt(t, client, heartbeat, 3)
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "the read-back attempts reached the cap")
			require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath), 3)
			require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1, "an accepted create is never sent again")
		})
	}
}

func TestCreateEnvelopeRefusalAfterALostCheckpointSelectsUncertain(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: respondJSON(http.StatusBadRequest, docusignError("TEMPLATE_ID_INVALID")),
		"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	result, err := sdkgo.RunMutation(newRetriedTestDexContext("refused-after-lost-checkpoint", 2), client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "the first attempt may have sent before Dex stored its checkpoint")
	require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath), 1)
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

func TestCreateEnvelopeResendsAnUnprovenCreateBelowTheCapAndSelectsUncertainAtTheCap(t *testing.T) {
	for name, test := range map[string]struct {
		isCreated                  bool
		isCustomFieldFilterIgnored bool
		isReadBackUnavailable      bool
		listAnswer                 http.HandlerFunc
	}{
		"not listed yet":             {isCreated: false},
		"filter ignored by DocuSign": {isCreated: true, isCustomFieldFilterIgnored: true},
		"listing rejected":           {isCreated: true, listAnswer: respondJSON(http.StatusBadRequest, docusignError("INVALID_REQUEST_PARAMETER"))},
		"listing malformed":          {isCreated: true, listAnswer: respondJSON(http.StatusOK, `{"envelopes":[{"envelopeId":"x"}]}`)},
		"listing unavailable":        {isCreated: true, isReadBackUnavailable: true, listAnswer: respondJSON(http.StatusServiceUnavailable, `{}`)},
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
			client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
			context := newTestDexContext("unproven")
			_, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the first of three possible sends resends on a later attempt")
			require.Contains(t, string(context.lastHeartbeat()), `"isDispatched":true`, "the next attempt reads back before it sends")
			require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)

			result, err := sdkgo.RunMutation(newRetriedTestDexContext("unproven", 3), client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
			if test.isReadBackUnavailable {
				require.ErrorAs(t, err, &retry, "an unavailable read-back retries before any send")
				require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
				return
			}
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "the third send reached the cap")
			require.Empty(t, result.Value.EnvelopeID)
			require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 2)
		})
	}
}

func TestCreateEnvelopeReadsBackBeforeEveryResendAndStopsAtTheCap(t *testing.T) {
	store := &fakeEnvelopeStore{}
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
			envelopeID := store.create(request)
			writeJSON(response, http.StatusCreated, strings.ReplaceAll(createdEnvelopeJSON("sent"), testEnvelopeID, envelopeID))
		},
		"GET " + envelopesPath: store.list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	checkpoint := map[string]any{"isDispatched": true, "dispatchedAt": fixedNow.Add(-time.Minute)}

	atCap := newRetriedTestDexContext("retried", 4)
	require.NoError(t, atCap.RecordHeartbeat(checkpoint))
	result, err := sdkgo.RunMutation(atCap, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "three earlier attempts may each have sent")
	require.Empty(t, fake.requestsTo(http.MethodPost, envelopesPath))
	require.Contains(t, string(atCap.lastHeartbeat()), `"isDispatched":true`)

	belowCap := newRetriedTestDexContext("retried", 2)
	require.NoError(t, belowCap.RecordHeartbeat(checkpoint))
	result, err = sdkgo.RunMutation(belowCap, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
	require.False(t, result.Value.WasRecovered, "the read-back missed, so this attempt resent")
	reads := fake.requestsTo(http.MethodGet, envelopesPath)
	require.Len(t, reads, 2)
	require.Equal(t, "2026-10-04T10:59:00Z", reads[1].query.Get("from_date"), "the read-back starts an hour before the earlier dispatch")
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
	require.Contains(t, string(belowCap.lastHeartbeat()), `"dispatchedAt":"2026-10-04T11:59:00Z"`, "the resend keeps the earliest dispatch time")

	result, err = sdkgo.RunMutation(newRetriedTestDexContext("retried", 4), client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
	require.True(t, result.Value.WasRecovered)
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
}

func TestCreateEnvelopeRefusalAfterAnEarlierDispatchSelectsUncertain(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: respondJSON(http.StatusBadRequest, docusignError("TEMPLATE_ID_INVALID")),
		"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	context := newRetriedTestDexContext("refused-resend", 2)
	require.NoError(t, context.RecordHeartbeat(map[string]any{"isDispatched": true, "dispatchedAt": fixedNow}))
	result, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "the earlier attempt's create may exist")
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
}

func TestCreateEnvelopeConflictIsAnUnknownOutcome(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: respondJSON(http.StatusConflict, docusignError("UNSPECIFIED_ERROR")),
		"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
	})
	_, err := sdkgo.RunMutation(newTestDexContext("conflict"), newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}).CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath), 1, "a 409 reads back before resending")
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
		"connection credentials missing": {
			credentials: func() docusign.CredentialSource { return failingCredentials{err: projectconfig.ErrObjectNotFound} },
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, test.routes)
			var credentials docusign.CredentialSource = staticCredentials(productionCredentials())
			if test.credentials != nil {
				credentials = test.credentials()
			}
			client := newTestClient(t, fake, credentials, test.config)
			for _, attempt := range []int32{2, 3} {
				context := newRetriedTestDexContext("dispatched-then-unauthorized", attempt)
				require.NoError(t, context.RecordHeartbeat(map[string]any{"isDispatched": true, "dispatchedAt": fixedNow.Add(-time.Minute)}))
				result, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
				require.NoError(t, err)
				require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "a terminal failure after a possible send selects uncertain, attempt %d", attempt)
				require.Contains(t, result.Failure.Message, "an earlier attempt may have sent the DocuSign create request")
				require.Contains(t, string(context.lastHeartbeat()), `"isDispatched":true`, "the checkpoint survives")
				requireNoSecrets(t, result)
			}
			require.Empty(t, fake.requestsTo(http.MethodPost, envelopesPath))
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

// failingCredentials fails every Resolve with err.
type failingCredentials struct{ err error }

func (credentials failingCredentials) Resolve(sdkgo.Call) (docusign.Credentials, error) {
	return docusign.Credentials{}, credentials.err
}

func (credentials failingCredentials) ResolveWithRefresh(
	context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[docusign.Credentials],
) (docusign.Credentials, error) {
	return docusign.Credentials{}, credentials.err
}

func TestCreateEnvelopeClassifiesCredentialFailuresBeforeTheFirstSend(t *testing.T) {
	for name, test := range map[string]struct {
		err        error
		wantBranch sdkgo.BranchID
	}{
		"broker unavailable":     {err: projectconfig.ErrCredentialBrokerUnavailable},
		"credentials missing":    {err: projectconfig.ErrObjectNotFound, wantBranch: docusign.CreateEnvelopeFromTemplateBranchDefect},
		"connection unselected":  {err: projectconfig.ErrConnectionNotSelected, wantBranch: docusign.CreateEnvelopeFromTemplateBranchDefect},
		"broker rejected":        {err: projectconfig.ErrCredentialBrokerRejected, wantBranch: docusign.CreateEnvelopeFromTemplateBranchDefect},
		"reauthorization needed": {err: sdkgo.ErrReauthorizationRequired, wantBranch: docusign.CreateEnvelopeFromTemplateBranchDefect},
		"project reauthorization": {err: projectconfig.ErrReauthorizationRequired,
			wantBranch: docusign.CreateEnvelopeFromTemplateBranchDefect},
		"not configured": {err: errors.New("connection is not configured")},
		"unlisted":       {err: errors.New("unlisted resolve failure")},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, nil)
			client := newTestClient(t, fake, failingCredentials{err: test.err}, docusign.Config{})
			context := newTestDexContext("credentials-" + name)
			result, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
			if test.wantBranch == "" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "a broker that cannot answer yet retries")
				require.Contains(t, string(context.lastHeartbeat()), `"sendCount":0`, "nothing was sent")
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantBranch, result.Branch)
			}
			require.Empty(t, fake.recordedRequests())
		})
	}
}

func TestCreateEnvelopeAcceptedCreateWhoseMarkerIsLostSelectsUncertain(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: respondJSON(http.StatusCreated, `{"envelopeId":`),
		"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	context := newTestDexContext("accepted-marker-lost")
	context.failingHeartbeatNumber = 2
	result, err := sdkgo.RunMutation(context, client.CreateEnvelopeFromTemplate(), testConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "a later attempt could not know the create was accepted")
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath), 1)
}

func TestCreateEnvelopeRefusalWithAnIncompleteBodyIsAnUnknownOutcome(t *testing.T) {
	for name, answer := range map[string]http.HandlerFunc{
		"oversized refusal body": respondJSON(http.StatusBadRequest, docusignError("TEMPLATE_ID_INVALID")+strings.Repeat(" ", providerhttp.MaxErrorBodyBytes)),
		"refusal body cut off":   cutOffBody(http.StatusBadRequest, docusignError("TEMPLATE_ID_INVALID")),
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"POST " + envelopesPath: answer,
				"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
			})
			client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
			_, heartbeat, err := runCreateAttempt(t, client, nil, 1)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the first of three possible sends resends on a later attempt")
			require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath), 1, "the unknown create is read back")

			_, heartbeat, err = runCreateAttempt(t, client, heartbeat, 2)
			require.ErrorAs(t, err, &retry)
			result, _, err := runCreateAttempt(t, client, heartbeat, 3)
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch, "the third send reached the cap")
			require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 3)
		})
	}
}

func TestCreateEnvelopeIncompleteUnauthorizedBodyIsNotSentAgainAfterARefresh(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: cutOffBody(http.StatusUnauthorized, docusignError("USER_AUTHENTICATION_FAILED")),
		"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
		"POST /oauth/token":     respondJSON(http.StatusOK, `{"access_token":"new-access","token_type":"Bearer","expires_in":28800}`),
	})
	client := newTestClient(t, fake, testsupport.NewRefreshingCredentialSource(productionCredentials(), nil), docusign.Config{})
	_, _, err := runCreateAttempt(t, client, nil, 1)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "an incomplete 401 body proves no refusal")
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1, "the create is not sent again with a refreshed token")
}

// envelopeNamingRefusal stores the create, then answers status with a body that names the new envelope.
func envelopeNamingRefusal(store *fakeEnvelopeStore, status int) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		envelopeID := store.create(request)
		writeJSON(response, status, `{"errorCode":"UNSPECIFIED_ERROR","message":"`+sentinelMessage+`","envelopeId":"`+envelopeID+`"}`)
	}
}

// readStoredEnvelope answers GET /envelopes/{id} from store.
func readStoredEnvelope(store *fakeEnvelopeStore, envelopeID string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		store.mu.Lock()
		marker, isFound := store.envelopes[envelopeID]
		store.mu.Unlock()
		if !isFound {
			writeJSON(response, http.StatusNotFound, docusignError("ENVELOPE_DOES_NOT_EXIST"))
			return
		}
		writeJSON(response, http.StatusOK, envelopeJSON(envelopeID, "sent", marker))
	}
}

func TestCreateEnvelopeRefusalNamingAnEnvelopeIsAcceptedAndReadBackByID(t *testing.T) {
	for name, status := range map[string]int{"422": http.StatusUnprocessableEntity, "400": http.StatusBadRequest, "500": http.StatusInternalServerError} {
		t.Run(name, func(t *testing.T) {
			store := &fakeEnvelopeStore{}
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"POST " + envelopesPath:                       envelopeNamingRefusal(store, status),
				"GET " + envelopesPath + "/" + testEnvelopeID: readStoredEnvelope(store, testEnvelopeID),
				"GET " + envelopesPath:                        (&fakeEnvelopeStore{}).list,
			})
			client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
			result, _, err := runCreateAttempt(t, client, nil, 1)
			require.NoError(t, err)
			require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
			require.Equal(t, testEnvelopeID, result.Value.EnvelopeID)
			require.True(t, result.Value.WasRecovered)
			require.Len(t, fake.requestsTo(http.MethodGet, envelopesPath+"/"+testEnvelopeID), 1, "the named envelope is read by its ID")
			require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
		})
	}
}

func TestCreateEnvelopeRefusalNamingAnEnvelopeIsNeverSentAgain(t *testing.T) {
	store := &fakeEnvelopeStore{}
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: envelopeNamingRefusal(store, http.StatusUnprocessableEntity),
		"GET " + envelopesPath:  (&fakeEnvelopeStore{}).list,
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	_, heartbeat, err := runCreateAttempt(t, client, nil, 1)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the read-backs missed, so a later attempt reads back again")
	require.Contains(t, string(heartbeat), `"isAccepted":true`)
	_, heartbeat, err = runCreateAttempt(t, client, heartbeat, 2)
	require.ErrorAs(t, err, &retry)
	result, _, err := runCreateAttempt(t, client, heartbeat, 3)
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchUncertain, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1, "an envelope-naming answer is never sent again")
}

func TestCreateEnvelopeUnauthorizedBodyNamingAnEnvelopeIsNotSentAgainAfterARefresh(t *testing.T) {
	store := &fakeEnvelopeStore{}
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath:                       envelopeNamingRefusal(store, http.StatusUnauthorized),
		"GET " + envelopesPath + "/" + testEnvelopeID: readStoredEnvelope(store, testEnvelopeID),
		"GET " + envelopesPath:                        (&fakeEnvelopeStore{}).list,
		"POST /oauth/token":                           respondJSON(http.StatusOK, `{"access_token":"new-access","token_type":"Bearer","expires_in":28800}`),
	})
	client := newTestClient(t, fake, testsupport.NewRefreshingCredentialSource(productionCredentials(), nil), docusign.Config{})
	result, _, err := runCreateAttempt(t, client, nil, 1)
	require.NoError(t, err)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1, "the create is not sent again with a refreshed token")
}
