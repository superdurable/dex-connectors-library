// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braze

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateUserProfileInput sets only supplied absolute attributes on one external user.
type UpdateUserProfileInput struct {
	// ExternalID identifies the profile and may create it when absent.
	ExternalID string `json:"externalId"`
	// FirstName replaces the given name when supplied.
	FirstName *string `json:"firstName,omitempty"`
	// LastName replaces the family name when supplied.
	LastName *string `json:"lastName,omitempty"`
	// Email replaces the contact email when supplied.
	Email *string `json:"email,omitempty"`
}

// UserWriteResult identifies the profile whose update Braze accepted for processing.
type UserWriteResult struct {
	// ExternalID is the submitted application user ID.
	ExternalID string `json:"externalId"`
}

// DeleteUserProfileInput selects one user profile for permanent deletion.
type DeleteUserProfileInput struct {
	// ExternalID is the application's user ID.
	ExternalID string `json:"externalId"`
}

// UserDeleteResult reports how many user profiles Braze queued for deletion.
type UserDeleteResult struct {
	// ExternalID is the submitted application user ID.
	ExternalID string `json:"externalId"`
	// Deleted is the number of matched profiles.
	Deleted int `json:"deleted"`
}

// ListCampaignsInput selects one page of up to 100 campaigns, beginning at zero.
type ListCampaignsInput struct {
	// Page is a non-negative zero-based page number.
	Page int `json:"page"`
}

// ListCanvasesInput selects one page of up to 100 Canvases, beginning at zero.
type ListCanvasesInput struct {
	// Page is a non-negative zero-based page number.
	Page int `json:"page"`
}

// ListSegmentsInput selects one page of up to 100 segments, beginning at zero.
type ListSegmentsInput struct {
	// Page is a non-negative zero-based page number.
	Page int `json:"page"`
}

// CampaignIdentity contains the public identity of a campaign.
type CampaignIdentity struct {
	// ID is the campaign API identifier.
	ID string `json:"id"`
	// Name is the campaign display name.
	Name string `json:"name"`
}

// CanvasIdentity contains the public identity of a Canvas.
type CanvasIdentity struct {
	// ID is the Canvas API identifier.
	ID string `json:"id"`
	// Name is the Canvas display name.
	Name string `json:"name"`
}

// SegmentIdentity contains the public identity and tracking setting of a segment.
type SegmentIdentity struct {
	// ID is the segment API identifier.
	ID string `json:"id"`
	// Name is the segment display name.
	Name string `json:"name"`
	// AnalyticsTrackingEnabled reports whether Braze tracks segment analytics.
	AnalyticsTrackingEnabled bool `json:"analyticsTrackingEnabled"`
}

// CampaignsList contains one campaign page.
type CampaignsList struct {
	// Campaigns is the page of campaign identities.
	Campaigns []CampaignIdentity `json:"campaigns"`
}

// CanvasesList contains one Canvas page.
type CanvasesList struct {
	// Canvases is the page of Canvas identities.
	Canvases []CanvasIdentity `json:"canvases"`
}

// SegmentsList contains one segment page.
type SegmentsList struct {
	// Segments is the page of segment identities.
	Segments []SegmentIdentity `json:"segments"`
}

// UpdateUserProfileOperation submits absolute profile attributes to Braze.
type UpdateUserProfileOperation struct{ client *Client }

// DeleteUserProfileOperation permanently deletes one profile by external ID.
type DeleteUserProfileOperation struct{ client *Client }

// ListCampaignsOperation retrieves campaign identities.
type ListCampaignsOperation struct{ client *Client }

// ListCanvasesOperation retrieves Canvas identities.
type ListCanvasesOperation struct{ client *Client }

// ListSegmentsOperation retrieves segment identities.
type ListSegmentsOperation struct{ client *Client }

// UpdateUserProfile binds the profile update to this client.
func (client *Client) UpdateUserProfile() UpdateUserProfileOperation {
	return UpdateUserProfileOperation{client}
}

// DeleteUserProfile binds the user deletion to this client.
func (client *Client) DeleteUserProfile() DeleteUserProfileOperation {
	return DeleteUserProfileOperation{client}
}

// ListCampaigns binds campaign discovery to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// ListCanvases binds Canvas discovery to this client.
func (client *Client) ListCanvases() ListCanvasesOperation { return ListCanvasesOperation{client} }

// ListSegments binds segment discovery to this client.
func (client *Client) ListSegments() ListSegmentsOperation { return ListSegmentsOperation{client} }

// Definition returns the generated profile update contract.
func (UpdateUserProfileOperation) Definition() sdkgo.MutationDefinition {
	return UpdateUserProfileDefinition
}

// Definition returns the generated deletion contract.
func (DeleteUserProfileOperation) Definition() sdkgo.MutationDefinition {
	return DeleteUserProfileDefinition
}

// Definition returns the generated campaign discovery contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Definition returns the generated Canvas discovery contract.
func (ListCanvasesOperation) Definition() sdkgo.QueryDefinition { return ListCanvasesDefinition }

// Definition returns the generated segment discovery contract.
func (ListSegmentsOperation) Definition() sdkgo.QueryDefinition { return ListSegmentsDefinition }

// IdempotencyKey preserves the call identity across profile update attempts.
func (UpdateUserProfileOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateUserProfileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves the call identity across deletion attempts.
func (DeleteUserProfileOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteUserProfileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke queues an absolute profile update, with no events or purchases.
func (operation UpdateUserProfileOperation) Invoke(call sdkgo.Call, input UpdateUserProfileInput) sdkgo.MutationAttempt[UserWriteResult] {
	const operationName = "updateUserProfile"
	if !validExternalID(input.ExternalID) || input.FirstName == nil && input.LastName == nil && input.Email == nil {
		return brazeMutationBranch(UpdateUserProfileBranchDefect, UserWriteResult{}, operationName, sdkgo.FailureValidation, "external ID and at least one attribute are required")
	}
	if input.Email != nil {
		address, err := mail.ParseAddress(*input.Email)
		if err != nil || address.Address != *input.Email {
			return brazeMutationBranch(UpdateUserProfileBranchDefect, UserWriteResult{}, operationName, sdkgo.FailureValidation, "email must be a plain address")
		}
	}
	attributes := struct {
		ExternalID string  `json:"external_id"`
		FirstName  *string `json:"first_name,omitempty"`
		LastName   *string `json:"last_name,omitempty"`
		Email      *string `json:"email,omitempty"`
	}{input.ExternalID, input.FirstName, input.LastName, input.Email}
	payload, err := json.Marshal(struct {
		Attributes []any `json:"attributes"`
	}{[]any{attributes}})
	if err != nil || len(payload) > 1<<20 {
		return brazeMutationBranch(UpdateUserProfileBranchDefect, UserWriteResult{}, operationName, sdkgo.FailureValidation, "profile update is invalid or too large")
	}
	response := operation.client.post(call, "/users/track", payload)
	if attempt := classifyBrazeMutationResponse[UserWriteResult](response, operationName, UpdateUserProfileBranchProviderRejected, UpdateUserProfileBranchInvalidResponse, UpdateUserProfileBranchDefect, http.StatusCreated); attempt != nil {
		return *attempt
	}
	var wire struct {
		Message string            `json:"message"`
		Errors  []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Message != "success" || len(wire.Errors) != 0 {
		return brazeMutationBranch(UpdateUserProfileBranchInvalidResponse, UserWriteResult{}, operationName, sdkgo.FailureProtocol, "Braze returned an invalid update acknowledgment")
	}
	return sdkgo.NewMutationBranch(UpdateUserProfileBranchAccepted, UserWriteResult{ExternalID: input.ExternalID}, nil, sdkgo.Receipt{})
}

// Invoke queues permanent deletion of one external user ID.
func (operation DeleteUserProfileOperation) Invoke(call sdkgo.Call, input DeleteUserProfileInput) sdkgo.MutationAttempt[UserDeleteResult] {
	const operationName = "deleteUserProfile"
	if !validExternalID(input.ExternalID) {
		return brazeMutationBranch(DeleteUserProfileBranchDefect, UserDeleteResult{}, operationName, sdkgo.FailureValidation, "external ID must be a nonempty UTF-8 value of at most 987 bytes")
	}
	payload, err := json.Marshal(struct {
		ExternalIDs []string `json:"external_ids"`
	}{[]string{input.ExternalID}})
	if err != nil {
		return brazeMutationBranch(DeleteUserProfileBranchDefect, UserDeleteResult{}, operationName, sdkgo.FailureLocalDefect, "deletion request could not be encoded")
	}
	response := operation.client.post(call, "/users/delete", payload)
	if attempt := classifyBrazeMutationResponse[UserDeleteResult](response, operationName, DeleteUserProfileBranchProviderRejected, DeleteUserProfileBranchInvalidResponse, DeleteUserProfileBranchDefect, http.StatusCreated); attempt != nil {
		return *attempt
	}
	var wire struct {
		Message string `json:"message"`
		Deleted *int   `json:"deleted"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Message != "success" || wire.Deleted == nil || *wire.Deleted < 0 || *wire.Deleted > 1 {
		return brazeMutationBranch(DeleteUserProfileBranchInvalidResponse, UserDeleteResult{}, operationName, sdkgo.FailureProtocol, "Braze returned an invalid deletion acknowledgment")
	}
	return sdkgo.NewMutationBranch(DeleteUserProfileBranchDeleted, UserDeleteResult{ExternalID: input.ExternalID, Deleted: *wire.Deleted}, nil, sdkgo.Receipt{})
}

// Invoke retrieves one page of campaigns without message content.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[CampaignsList] {
	return invokeBrazeList(call, operation.client, "listCampaigns", "/campaigns/list", input.Page, "campaigns", ListCampaignsBranchListed, ListCampaignsBranchProviderRejected, ListCampaignsBranchInvalidResponse, ListCampaignsBranchDefect, func(body []byte) (CampaignsList, error) {
		var wire struct {
			Message   string             `json:"message"`
			Campaigns []CampaignIdentity `json:"campaigns"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Message != "success" || wire.Campaigns == nil || !validIdentities(wire.Campaigns) {
			return CampaignsList{}, errors.New("invalid campaign list")
		}
		return CampaignsList{Campaigns: wire.Campaigns}, nil
	})
}

// Invoke retrieves one page of Canvases without message content.
func (operation ListCanvasesOperation) Invoke(call sdkgo.Call, input ListCanvasesInput) sdkgo.QueryAttempt[CanvasesList] {
	return invokeBrazeList(call, operation.client, "listCanvases", "/canvas/list", input.Page, "canvases", ListCanvasesBranchListed, ListCanvasesBranchProviderRejected, ListCanvasesBranchInvalidResponse, ListCanvasesBranchDefect, func(body []byte) (CanvasesList, error) {
		var wire struct {
			Message  string           `json:"message"`
			Canvases []CanvasIdentity `json:"canvases"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Message != "success" || wire.Canvases == nil || !validCanvasIdentities(wire.Canvases) {
			return CanvasesList{}, errors.New("invalid Canvas list")
		}
		return CanvasesList{Canvases: wire.Canvases}, nil
	})
}

// Invoke retrieves one page of segments and their analytics settings.
func (operation ListSegmentsOperation) Invoke(call sdkgo.Call, input ListSegmentsInput) sdkgo.QueryAttempt[SegmentsList] {
	return invokeBrazeList(call, operation.client, "listSegments", "/segments/list", input.Page, "segments", ListSegmentsBranchListed, ListSegmentsBranchProviderRejected, ListSegmentsBranchInvalidResponse, ListSegmentsBranchDefect, func(body []byte) (SegmentsList, error) {
		var wire struct {
			Message  string `json:"message"`
			Segments []struct {
				ID                       string `json:"id"`
				Name                     string `json:"name"`
				AnalyticsTrackingEnabled bool   `json:"analytics_tracking_enabled"`
			} `json:"segments"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Message != "success" || wire.Segments == nil {
			return SegmentsList{}, errors.New("invalid segment list")
		}
		segments := make([]SegmentIdentity, 0, len(wire.Segments))
		for _, segment := range wire.Segments {
			if segment.ID == "" || segment.Name == "" {
				return SegmentsList{}, errors.New("missing segment identity")
			}
			segments = append(segments, SegmentIdentity{ID: segment.ID, Name: segment.Name, AnalyticsTrackingEnabled: segment.AnalyticsTrackingEnabled})
		}
		return SegmentsList{Segments: segments}, nil
	})
}

func validExternalID(externalID string) bool {
	return strings.TrimSpace(externalID) != "" && utf8.ValidString(externalID) && len(externalID) <= 987
}

func validIdentities(identities []CampaignIdentity) bool {
	for _, identity := range identities {
		if identity.ID == "" || identity.Name == "" {
			return false
		}
	}
	return true
}

func validCanvasIdentities(identities []CanvasIdentity) bool {
	for _, identity := range identities {
		if identity.ID == "" || identity.Name == "" {
			return false
		}
	}
	return true
}

func invokeBrazeList[T any](call sdkgo.Call, client *Client, operationName, endpoint string, page int, resource string, listed, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	if page < 0 {
		return brazeQueryBranch(defect, zero, operationName, sdkgo.FailureValidation, "page must be non-negative")
	}
	response := client.get(call, endpoint+"?"+url.Values{"page": []string{strconv.Itoa(page)}}.Encode())
	if errors.Is(response.err, errCredential) {
		return brazeQueryBranch(defect, zero, operationName, sdkgo.FailureAuthentication, "Braze credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](brazeFailure(operationName, sdkgo.FailureTransport, "Braze "+resource+" request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](brazeFailure(operationName, sdkgo.FailureRateLimit, "Braze rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](brazeFailure(operationName, sdkgo.FailureAvailability, "Braze is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return brazeQueryBranch(rejected, zero, operationName, sdkgo.FailureProviderRejection, "Braze rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return brazeQueryBranch(invalid, zero, operationName, sdkgo.FailureResponseTooLarge, "Braze response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](brazeFailure(operationName, sdkgo.FailureTransport, "Braze response was interrupted"), 0)
	}
	value, err := decode(response.body)
	if err != nil {
		return brazeQueryBranch(invalid, zero, operationName, sdkgo.FailureProtocol, "Braze returned invalid "+resource+" JSON")
	}
	return sdkgo.NewQueryBranch(listed, value, nil, sdkgo.Receipt{})
}

func classifyBrazeMutationResponse[T any](response providerResponse, operationName string, rejected, invalid, defect sdkgo.BranchID, expectedStatus int) *sdkgo.MutationAttempt[T] {
	var zero T
	var attempt sdkgo.MutationAttempt[T]
	switch {
	case errors.Is(response.err, errCredential):
		attempt = brazeMutationBranch(defect, zero, operationName, sdkgo.FailureAuthentication, "Braze credentials are unavailable or invalid")
	case errors.Is(response.err, errRequest):
		attempt = sdkgo.NewMutationRetry[T](brazeFailure(operationName, sdkgo.FailureTransport, "Braze response is unavailable"), 0)
	case response.status == http.StatusTooManyRequests:
		attempt = sdkgo.NewMutationRetry[T](brazeFailure(operationName, sdkgo.FailureRateLimit, "Braze rate limited the request"), response.retryAfter)
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		attempt = sdkgo.NewMutationRetry[T](brazeFailure(operationName, sdkgo.FailureAvailability, "Braze is temporarily unavailable"), response.retryAfter)
	case response.status != expectedStatus:
		attempt = brazeMutationBranch(rejected, zero, operationName, sdkgo.FailureProviderRejection, "Braze rejected HTTP "+strconv.Itoa(response.status))
	case errors.Is(response.err, providerhttp.ErrBodyTooLarge):
		attempt = brazeMutationBranch(invalid, zero, operationName, sdkgo.FailureResponseTooLarge, "Braze response exceeded the configured limit")
	case response.err != nil:
		attempt = sdkgo.NewMutationRetry[T](brazeFailure(operationName, sdkgo.FailureTransport, "Braze response was interrupted"), 0)
	default:
		return nil
	}
	return &attempt
}

func brazeQueryBranch[T any](branch sdkgo.BranchID, value T, operationName string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := brazeFailure(operationName, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func brazeMutationBranch[T any](branch sdkgo.BranchID, value T, operationName string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := brazeFailure(operationName, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func brazeFailure(operationName string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "braze", Operation: operationName, Message: message}
}
