// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateFeatureInput supplies the name of a new Productboard feature.
type CreateFeatureInput struct {
	// Name is the nonempty feature name in the workspace.
	Name string `json:"name"`
	// ParentComponentID is the UUID of the component that will contain the feature.
	ParentComponentID string `json:"parentComponentId"`
}

// CreateFeatureOperation adds one feature with a bounded send count.
type CreateFeatureOperation struct{ client *Client }

// CreateFeature binds the Mutation to this client.
func (client *Client) CreateFeature() CreateFeatureOperation { return CreateFeatureOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateFeatureOperation) Definition() sdkgo.MutationDefinition { return CreateFeatureDefinition }

// IdempotencyKey records call identity; Productboard does not deduplicate it.
func (CreateFeatureOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateFeatureInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one feature and retries uncertain sends only within the configured cap.
func (operation CreateFeatureOperation) Invoke(call sdkgo.Call, input CreateFeatureInput) sdkgo.MutationAttempt[FeatureMetadata] {
	if !validFeatureName(input.Name) || !validFeatureID(input.ParentComponentID) {
		return featureMutationBranch(CreateFeatureBranchDefect, "createFeature", sdkgo.FailureValidation, "feature name or parent component ID is invalid", FeatureMetadata{})
	}
	fence, err := singledispatch.NewFence(call, CreateFeatureDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return featureMutationBranch(CreateFeatureBranchDefect, "createFeature", sdkgo.FailureLocalDefect, "Productboard dispatch checkpoint is unavailable", FeatureMetadata{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureAvailability, "Productboard dispatch checkpoint could not be trusted"), 0)
	}
	if decision.Marker.ProviderID != "" {
		response := operation.client.get(call, "/entities/"+decision.Marker.ProviderID+"?fields%5B%5D=name")
		if response.status == http.StatusOK && response.err == nil {
			if feature, isValid := decodeFeature(response.body); isValid && feature.ID == decision.Marker.ProviderID && feature.Name == input.Name {
				return sdkgo.NewMutationBranch(CreateFeatureBranchCreated, feature, nil, sdkgo.Receipt{})
			}
		}
		return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureAvailability, "Productboard feature creation could not be confirmed by ID"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return createFeatureUncertain(sdkgo.FailureTransport, "Productboard feature creation could not be confirmed")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return featureMutationBranch(CreateFeatureBranchDefect, "createFeature", sdkgo.FailureAuthentication, "Productboard credentials are unavailable", FeatureMetadata{})
	}
	payload, err := json.Marshal(map[string]any{"data": map[string]any{"type": "feature", "fields": map[string]string{"name": input.Name}, "relationships": []any{map[string]any{"type": "parent", "target": map[string]string{"type": "component", "id": input.ParentComponentID}}}}})
	if err != nil {
		return featureMutationBranch(CreateFeatureBranchDefect, "createFeature", sdkgo.FailureLocalDefect, "Productboard feature payload could not be encoded", FeatureMetadata{})
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/entities", bytes.NewReader(payload))
	if err != nil {
		return featureMutationBranch(CreateFeatureBranchDefect, "createFeature", sdkgo.FailureLocalDefect, "Productboard request could not be built", FeatureMetadata{})
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIToken.Reveal())
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return featureMutationBranch(CreateFeatureBranchDefect, "createFeature", sdkgo.FailureLocalDefect, "Productboard request is invalid", FeatureMetadata{})
	}
	dispatch := createFeatureDispatch{client: operation.client, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureTransport, "Productboard request was not sent"), 0)
		}
		if !dispatch.didGetConnection.Load() && fence.RecordCanResend() == nil {
			return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureTransport, "Productboard was unreachable before sending"), 0)
		}
		return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureTransport, "Productboard creation outcome is unknown"), 0)
	}
	defer func() { _ = dispatch.response.Body.Close() }()
	if dispatch.response.StatusCode == http.StatusTooManyRequests || dispatch.response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureAvailability, "Productboard retry checkpoint is unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if dispatch.response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", kind, "Productboard did not create the feature"), providerhttp.ParseRetryAfter(dispatch.response.Header.Get("Retry-After"), time.Now()))
	}
	if dispatch.response.StatusCode >= 500 || dispatch.response.StatusCode == http.StatusRequestTimeout {
		return createFeatureUnknownOutcome(call, decision, "Productboard creation outcome is unknown")
	}
	if dispatch.response.StatusCode != http.StatusCreated {
		body, readErr := providerhttp.ReadBoundedBody(dispatch.response.Body, operation.client.maxResponseBytes)
		if readErr != nil || !isDefiniteProductboardRefusal(dispatch.response.StatusCode, body) {
			return createFeatureUnknownOutcome(call, decision, "Productboard creation response is unknown")
		}
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureAvailability, "Productboard refusal checkpoint is unavailable"), 0)
		}
		if decision.MaximumPriorSends > 0 {
			return createFeatureUnknownOutcome(call, decision, "an earlier Productboard feature creation may have succeeded")
		}
		return featureMutationBranch(CreateFeatureBranchProviderRejected, "createFeature", sdkgo.FailureProviderRejection, "Productboard rejected feature creation", FeatureMetadata{})
	}
	body, err := providerhttp.ReadBoundedBody(dispatch.response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return createFeatureUnknownOutcome(call, decision, "Productboard feature response could not be read")
	}
	featureID, isValid := decodeCreatedFeatureID(body)
	if !isValid {
		return createFeatureUnknownOutcome(call, decision, "Productboard feature response returned no usable ID")
	}
	if err := fence.RecordProviderID(featureID); err != nil {
		return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureAvailability, "Productboard feature ID could not be checkpointed"), 0)
	}
	return sdkgo.NewMutationBranch(CreateFeatureBranchCreated, FeatureMetadata{ID: featureID, Name: input.Name}, nil, sdkgo.Receipt{})
}

func createFeatureUnknownOutcome(call sdkgo.Call, decision singledispatch.Decision, message string) sdkgo.MutationAttempt[FeatureMetadata] {
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("createFeature", sdkgo.FailureTransport, message), 0)
	}
	return createFeatureUncertain(sdkgo.FailureTransport, message)
}

func isDefiniteProductboardRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var rejection struct {
		Message string `json:"message"`
	}
	return json.Unmarshal(body, &rejection) == nil && rejection.Message != ""
}

func decodeCreatedFeatureID(body []byte) (string, bool) {
	var wire struct {
		Data struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || !validFeatureID(wire.Data.ID) || wire.Data.Type != "feature" {
		return "", false
	}
	return wire.Data.ID, true
}

type createFeatureDispatch struct {
	client           *Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *createFeatureDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	request := dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace))
	response, err := dispatch.client.httpClient.Do(request)
	dispatch.response = response
	return err
}

func createFeatureUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[FeatureMetadata] {
	return sdkgo.NewMutationUncertain(FeatureMetadata{}, featureFailure("createFeature", kind, message), sdkgo.Receipt{})
}
