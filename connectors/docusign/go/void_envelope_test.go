// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

func voidEnvelope(t *testing.T, fake *fakeDocuSign, reason string) (docusign.VoidEnvelopeResult, error) {
	t.Helper()
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	return sdkgo.RunMutation(newTestDexContext("void"), client.VoidEnvelope(), testConnection,
		docusign.VoidEnvelopeInput{EnvelopeID: strings.ToUpper(testEnvelopeID), VoidedReason: reason})
}

func TestVoidEnvelopeSendsTheVoidedStatusAndReason(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"PUT " + envelopePath: respondJSON(http.StatusOK, `{"envelopeId":"`+testEnvelopeID+`"}`)})
	result, err := voidEnvelope(t, fake, "The signing deadline passed.")
	require.NoError(t, err)
	require.Equal(t, docusign.VoidEnvelopeBranchVoided, result.Branch)
	require.Equal(t, docusign.VoidedEnvelope{EnvelopeID: testEnvelopeID}, result.Value)
	puts := fake.requestsTo(http.MethodPut, envelopePath)
	require.Len(t, puts, 1)
	require.JSONEq(t, `{"status":"voided","voidedReason":"The signing deadline passed."}`, puts[0].body)
	require.Empty(t, fake.requestsTo(http.MethodGet, envelopePath))
}

func TestVoidEnvelopeReadsBackARefusedVoid(t *testing.T) {
	for name, test := range map[string]struct {
		status          string
		expectedBranch  sdkgo.BranchID
		isAlreadyVoided bool
	}{
		"already voided by an earlier attempt": {"voided", docusign.VoidEnvelopeBranchVoided, true},
		"completed":                            {"completed", docusign.VoidEnvelopeBranchNotVoidable, false},
		"declined":                             {"declined", docusign.VoidEnvelopeBranchNotVoidable, false},
		"draft":                                {"created", docusign.VoidEnvelopeBranchNotVoidable, false},
		"still in flight":                      {"sent", docusign.VoidEnvelopeBranchProviderRejected, false},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"PUT " + envelopePath: respondJSON(http.StatusBadRequest, docusignError("ENVELOPE_CANNOT_VOID_INVALID_STATE")),
				"GET " + envelopePath: respondJSON(http.StatusOK, strings.Replace(envelopeJSON(testEnvelopeID, test.status, "marker"), `"status"`, `"voidedReason":"Sender canceled","status"`, 1)),
			})
			result, err := voidEnvelope(t, fake, "Deal lost")
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.isAlreadyVoided, result.Value.WasAlreadyVoided)
			if test.isAlreadyVoided {
				require.Equal(t, "Sender canceled", result.Value.VoidedReason)
			}
			if test.expectedBranch == docusign.VoidEnvelopeBranchNotVoidable {
				require.Equal(t, docusign.EnvelopeStatus(test.status), result.Value.Status)
				require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
			}
			require.Len(t, fake.requestsTo(http.MethodGet, envelopePath), 1)
			requireNoSecrets(t, result)
		})
	}
}

func TestVoidEnvelopeRetriesAnUnreadableStatusReadBack(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"PUT " + envelopePath: respondJSON(http.StatusBadRequest, docusignError("ENVELOPE_CANNOT_VOID_INVALID_STATE")),
		"GET " + envelopePath: respondJSON(http.StatusOK, `{"envelopeId":`),
	})
	_, err := voidEnvelope(t, fake, "Deal lost")
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "voiding again is safe, so an unreadable read-back never selects invalidResponse")
	require.Equal(t, sdkgo.FailureProtocol, retry.Failure.Kind)
}

func TestVoidEnvelopeReadsBackBeforeRetryingAnUnknownWrite(t *testing.T) {
	var isVoided bool
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"PUT " + envelopePath: func(writer http.ResponseWriter, request *http.Request) {
			isVoided = true
			respondJSON(http.StatusInternalServerError, docusignError("UNSPECIFIED_ERROR"))(writer, request)
		},
		"GET " + envelopePath: func(writer http.ResponseWriter, request *http.Request) {
			status := "sent"
			if isVoided {
				status = "voided"
			}
			respondJSON(http.StatusOK, envelopeJSON(testEnvelopeID, status, "marker"))(writer, request)
		},
	})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	input := docusign.VoidEnvelopeInput{EnvelopeID: testEnvelopeID, VoidedReason: "Deal lost"}
	_, err := sdkgo.RunMutation(newTestDexContext("void-read-back"), client.VoidEnvelope(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	result, err := sdkgo.RunMutation(newRetriedTestDexContext("void-read-back", 2), client.VoidEnvelope(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, docusign.VoidEnvelopeBranchVoided, result.Branch)
	require.True(t, result.Value.WasAlreadyVoided)
	require.Len(t, fake.requestsTo(http.MethodPut, envelopePath), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, envelopePath), 1)
}

func TestVoidEnvelopeResendsAfterDefiniteReadBackRefusal(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		code   string
		branch sdkgo.BranchID
	}{
		{name: "missing envelope", status: http.StatusNotFound, code: "ENVELOPE_DOES_NOT_EXIST", branch: docusign.VoidEnvelopeBranchNotFound},
		{name: "permission denied", status: http.StatusForbidden, code: "USER_LACKS_PERMISSIONS", branch: docusign.VoidEnvelopeBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			answer := respondJSON(test.status, docusignError(test.code))
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"GET " + envelopePath: answer,
				"PUT " + envelopePath: answer,
			})
			client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
			result, err := sdkgo.RunMutation(newRetriedTestDexContext("void-read-refusal", 2), client.VoidEnvelope(), testConnection,
				docusign.VoidEnvelopeInput{EnvelopeID: testEnvelopeID, VoidedReason: "Deal lost"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Len(t, fake.requestsTo(http.MethodGet, envelopePath), 1)
			require.Len(t, fake.requestsTo(http.MethodPut, envelopePath), 1)
		})
	}
}

func TestVoidEnvelopeClassifiesOtherAnswers(t *testing.T) {
	for name, test := range map[string]struct {
		status         int
		body           string
		expectedBranch sdkgo.BranchID
	}{
		"missing envelope":  {http.StatusBadRequest, docusignError("ENVELOPE_DOES_NOT_EXIST"), docusign.VoidEnvelopeBranchNotFound},
		"not the sender":    {http.StatusBadRequest, docusignError("USER_LACKS_PERMISSIONS"), docusign.VoidEnvelopeBranchProviderRejected},
		"oversized summary": {http.StatusOK, `{"envelopeId":"` + strings.Repeat("x", 2<<20) + `"}`, docusign.VoidEnvelopeBranchVoided},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"PUT " + envelopePath: respondJSON(test.status, test.body)})
			result, err := voidEnvelope(t, fake, "Deal lost")
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
		})
	}

	for _, status := range []int{http.StatusServiceUnavailable, http.StatusConflict, http.StatusFound, http.StatusGone} {
		fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"PUT " + envelopePath: respondJSON(status, docusignError("UNSPECIFIED_ERROR"))})
		_, err := voidEnvelope(t, fake, "Deal lost")
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "a void is safe to repeat after HTTP %d", status)
	}

	for name, answer := range map[string]http.HandlerFunc{
		"oversized refusal body": respondJSON(http.StatusBadRequest, docusignError("USER_LACKS_PERMISSIONS")+strings.Repeat(" ", providerhttp.MaxErrorBodyBytes)),
		"refusal body cut off":   cutOffBody(http.StatusBadRequest, docusignError("USER_LACKS_PERMISSIONS")),
	} {
		fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"PUT " + envelopePath: answer})
		_, err := voidEnvelope(t, fake, "Deal lost")
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "an incomplete refusal body proves no refusal: %s", name)
	}
}

func TestVoidEnvelopeRequiresAReasonDocuSignKeepsWhole(t *testing.T) {
	for _, reason := range []string{"", "   ", strings.Repeat("r", 201)} {
		fake := newFakeDocuSign(t, nil)
		result, err := voidEnvelope(t, fake, reason)
		require.NoError(t, err)
		require.Equal(t, docusign.VoidEnvelopeBranchDefect, result.Branch)
		require.Empty(t, fake.recordedRequests())
	}
}
