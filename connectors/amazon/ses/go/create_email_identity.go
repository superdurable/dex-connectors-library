// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createEmailIdentityOperationID = "createEmailIdentity"

// CreateEmailIdentityInput starts verification of one email address or domain.
type CreateEmailIdentityInput struct {
	// EmailIdentity is the address or domain to add under SES Console > Verified identities.
	EmailIdentity string `json:"emailIdentity"`
}

// CreatedEmailIdentity describes the identity accepted for verification.
type CreatedEmailIdentity struct {
	// Name is the email address or domain.
	Name string `json:"name"`
	// IdentityType is EMAIL_ADDRESS or DOMAIN.
	IdentityType string `json:"identityType"`
	// VerifiedForSending is false until the verification link or DNS records are confirmed.
	VerifiedForSending bool `json:"verifiedForSending"`
	// DKIMTokens are public CNAME token values for domain verification, when SES supplies them.
	DKIMTokens []string `json:"dkimTokens,omitempty"`
}

// CreateEmailIdentityOperation implements bounded identity creation.
type CreateEmailIdentityOperation struct{ client *Client }

// CreateEmailIdentity binds the Mutation to this client.
func (client *Client) CreateEmailIdentity() CreateEmailIdentityOperation {
	return CreateEmailIdentityOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateEmailIdentityOperation) Definition() sdkgo.MutationDefinition {
	return CreateEmailIdentityDefinition
}

// IdempotencyKey identifies retries of this Step; SES does not deduplicate by it.
func (CreateEmailIdentityOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEmailIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads back after an unknown outcome and limits possible verification requests.
func (operation CreateEmailIdentityOperation) Invoke(call sdkgo.Call, input CreateEmailIdentityInput) sdkgo.MutationAttempt[CreatedEmailIdentity] {
	if !emailIdentityPattern.MatchString(input.EmailIdentity) || !utf8.ValidString(input.EmailIdentity) {
		return createIdentityBranch(CreateEmailIdentityBranchDefect, sdkgo.FailureValidation, "SES email identity name is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateEmailIdentityDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createIdentityBranch(CreateEmailIdentityBranchDefect, sdkgo.FailureLocalDefect, "SES identity creation fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(CreatedEmailIdentity{}, createIdentityFailure(sdkgo.FailureTransport, "SES identity creation checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		if value, isConfirmed := operation.readBack(call, input.EmailIdentity); isConfirmed {
			return sdkgo.NewMutationBranch(CreateEmailIdentityBranchCreated, value, nil, createIdentityReceipt(call, input.EmailIdentity))
		}
		if fence.Decide().Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(CreatedEmailIdentity{}, createIdentityFailure(sdkgo.FailureTransport, "SES identity creation could not be confirmed after the send cap"), sdkgo.Receipt{})
		}
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(CreatedEmailIdentity{}, createIdentityFailure(sdkgo.FailureTransport, "SES unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return createIdentityBranch(CreateEmailIdentityBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		EmailIdentity string `json:"EmailIdentity"`
	}{EmailIdentity: input.EmailIdentity})
	if err != nil {
		return createIdentityBranch(CreateEmailIdentityBranchDefect, sdkgo.FailureLocalDefect, "SES identity request could not be encoded")
	}
	send := &sesIdentityCreate{client: operation.client, call: call, body: body}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationUncertain(CreatedEmailIdentity{}, createIdentityFailure(sdkgo.FailureTransport, "SES identity creation dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(CreatedEmailIdentity{}, createIdentityFailure(sdkgo.FailureTransport, "SES resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CreatedEmailIdentity](createIdentityFailure(kind, "SES asked the connector to retry identity creation"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CreatedEmailIdentity](createIdentityFailure(sdkgo.FailureTransport, "SES identity creation outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return createIdentityBranch(CreateEmailIdentityBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected identity creation")
	}
	value, isValid := decodeCreatedIdentity(response.body, input.EmailIdentity)
	if !isValid {
		return sdkgo.NewMutationRetry[CreatedEmailIdentity](createIdentityFailure(sdkgo.FailureProtocol, "SES identity creation response was invalid"), 0)
	}
	return sdkgo.NewMutationBranch(CreateEmailIdentityBranchCreated, value, nil, createIdentityReceipt(call, input.EmailIdentity))
}

func (operation CreateEmailIdentityOperation) readBack(call sdkgo.Call, identity string) (CreatedEmailIdentity, bool) {
	response := operation.client.getEmailIdentity(call, identity)
	if response.status != http.StatusOK || response.err != nil {
		return CreatedEmailIdentity{}, false
	}
	return decodeCreatedIdentity(response.body, identity)
}

type sesIdentityCreate struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response providerResponse
}

func (send *sesIdentityCreate) dispatch(requestContext context.Context) error {
	send.response = send.client.requestWithContext(send.call, requestContext, http.MethodPost, "/v2/email/identities", send.body)
	return nil
}

func decodeCreatedIdentity(body []byte, identity string) (CreatedEmailIdentity, bool) {
	var wire struct {
		IdentityType             string `json:"IdentityType"`
		VerifiedForSendingStatus bool   `json:"VerifiedForSendingStatus"`
		DkimAttributes           struct {
			Tokens []string `json:"Tokens"`
		} `json:"DkimAttributes"`
	}
	if json.Unmarshal(body, &wire) != nil || (wire.IdentityType != "EMAIL_ADDRESS" && wire.IdentityType != "DOMAIN") || len(wire.DkimAttributes.Tokens) > 3 {
		return CreatedEmailIdentity{}, false
	}
	for _, token := range wire.DkimAttributes.Tokens {
		if token == "" || len(token) > 256 {
			return CreatedEmailIdentity{}, false
		}
	}
	return CreatedEmailIdentity{Name: identity, IdentityType: wire.IdentityType, VerifiedForSending: wire.VerifiedForSendingStatus, DKIMTokens: wire.DkimAttributes.Tokens}, true
}

func createIdentityReceipt(call sdkgo.Call, identity string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-ses", ProviderObjectID: identity}
}

func createIdentityFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-ses", Operation: createEmailIdentityOperationID, Message: message}
}

func createIdentityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedEmailIdentity] {
	failure := createIdentityFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, CreatedEmailIdentity{}, &failure, sdkgo.Receipt{})
}
