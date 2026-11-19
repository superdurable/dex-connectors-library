// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateOpportunityInput names a candidate and the Lever user performing the create.
type CreateOpportunityInput struct {
	// PerformAsUserID is a Lever user UID selected in Settings > Users.
	PerformAsUserID string `json:"performAsUserId"`
	// Name is the candidate's full display name.
	Name string `json:"name"`
	// Email is the candidate's email address used by Lever to deduplicate contacts.
	Email string `json:"email"`
}

// CreatedOpportunity identifies the confirmed new opportunity.
type CreatedOpportunity struct {
	// ID is the provider-assigned opportunity UID.
	ID string `json:"id"`
}

// CreateOpportunityOperation creates one candidate opportunity.
type CreateOpportunityOperation struct{ client *Client }

// CreateOpportunity binds the Mutation to this client.
func (client *Client) CreateOpportunity() CreateOpportunityOperation {
	return CreateOpportunityOperation{client: client}
}

// Definition returns the generated Mutation contract.
func (CreateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return CreateOpportunityDefinition
}

// IdempotencyKey correlates retries; Lever does not accept a deduplication header for creates.
func (CreateOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one fenced create attempt, then applies the configured bounded resend policy.
func (operation CreateOpportunityOperation) Invoke(call sdkgo.Call, input CreateOpportunityInput) sdkgo.MutationAttempt[CreatedOpportunity] {
	if !validOpportunityID(input.PerformAsUserID) || !validCandidateName(input.Name) || !validEmail(input.Email) {
		return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureValidation, "performing user, candidate name, or email is invalid", sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateOpportunityDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "Lever create checkpoint could not be initialized", sdkgo.Receipt{})
	}
	decision := fence.Decide()
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever"}
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(CreatedOpportunity{}, createOpportunityFailure(sdkgo.FailureLocalDefect, "Lever create checkpoint cannot prove whether an earlier send occurred"), receipt)
	}
	if decision.Marker.ProviderID != "" {
		response := operation.client.get(call, "/opportunities/"+url.PathEscape(decision.Marker.ProviderID))
		if response.status == http.StatusOK && response.err == nil {
			if created, err := decodeCreatedOpportunity(response.body); err == nil && created.ID == decision.Marker.ProviderID {
				receipt.ProviderObjectID = created.ID
				return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, created, nil, receipt)
			}
		}
		if response.status != http.StatusNotFound {
			return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(sdkgo.FailureAvailability, "Lever opportunity read-back is unavailable"), response.retryAfter)
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CreatedOpportunity{}, createOpportunityFailure(sdkgo.FailureTransport, "Lever create may have succeeded but no opportunity ID was confirmed at the send cap"), receipt)
	}
	payload, err := json.Marshal(struct {
		Name   string   `json:"name"`
		Emails []string `json:"emails"`
	}{Name: input.Name, Emails: []string{input.Email}})
	if err != nil {
		return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "Lever create request could not be encoded", receipt)
	}
	requestPath := "/opportunities?" + url.Values{"perform_as": {input.PerformAsUserID}}.Encode()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doWithContext(call, requestContext, http.MethodPost, requestPath, payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(sdkgo.FailureAvailability, "Lever create was not dispatched after a durable claim"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(sdkgo.FailureAvailability, "Lever retry checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(kind, "Lever temporarily refused the create request"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedOpportunity{}, createOpportunityFailure(sdkgo.FailureAuthentication, "Lever credentials are unavailable after a possible earlier create"), receipt)
		}
		return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable or invalid", receipt)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(sdkgo.FailureTransport, "Lever create outcome is unknown"), response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(CreatedOpportunity{}, createOpportunityFailure(sdkgo.FailureTransport, "Lever create outcome is unknown at the send cap"), receipt)
	}
	if response.status < 200 || response.status >= 300 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedOpportunity{}, createOpportunityFailure(sdkgo.FailureProviderRejection, "Lever rejected a resend after a possible earlier create"), receipt)
		}
		return createOpportunityBranch(CreateOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected the opportunity create", receipt)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(sdkgo.FailureResponseTooLarge, "Lever create response exceeded the limit after a possible create"), 0)
	}
	created, err := decodeCreatedOpportunity(response.body)
	if err != nil {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(sdkgo.FailureProtocol, "Lever create response did not confirm an ID"), 0)
		}
		return sdkgo.NewMutationUncertain(CreatedOpportunity{}, createOpportunityFailure(sdkgo.FailureProtocol, "Lever create response did not confirm an ID at the send cap"), receipt)
	}
	if err := fence.RecordProviderID(created.ID); err != nil {
		return sdkgo.NewMutationRetry[CreatedOpportunity](createOpportunityFailure(sdkgo.FailureAvailability, "Lever created the opportunity but its ID checkpoint could not be recorded"), 0)
	}
	receipt.ProviderObjectID = created.ID
	return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, created, nil, receipt)
}

func validCandidateName(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func decodeCreatedOpportunity(body []byte) (CreatedOpportunity, error) {
	var wire struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CreatedOpportunity{}, err
	}
	if !validOpportunityID(wire.Data.ID) {
		return CreatedOpportunity{}, errors.New("created opportunity ID missing")
	}
	return CreatedOpportunity{ID: wire.Data.ID}, nil
}

func createOpportunityFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: "createOpportunity", Message: message}
}

func createOpportunityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedOpportunity] {
	failure := createOpportunityFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, CreatedOpportunity{}, &failure, receipt)
}
