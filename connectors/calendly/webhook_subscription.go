// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// WebhookEventInviteeCreated is the Calendly webhook event sent when an invitee books an event.
	WebhookEventInviteeCreated = "invitee.created"
	// WebhookEventInviteeCanceled is the Calendly webhook event sent when an invitee or host cancels.
	WebhookEventInviteeCanceled = "invitee.canceled"

	// WebhookSubscriptionScopeUser subscribes to one user's events.
	WebhookSubscriptionScopeUser = "user"
	// WebhookSubscriptionScopeOrganization subscribes to every event of an organization; it needs an owner
	// or admin token.
	WebhookSubscriptionScopeOrganization = "organization"

	// WebhookSubscriptionStateActive is a subscription Calendly delivers to.
	WebhookSubscriptionStateActive = "active"

	// maximumWebhookSubscriptionPages bounds the list-before-create scan at 1,000 subscriptions.
	maximumWebhookSubscriptionPages = 10
)

// CreateWebhookSubscriptionInput registers one callback URL for invitee webhooks.
type CreateWebhookSubscriptionInput struct {
	// CallbackURL is the public HTTPS URL where the application mounts the inviteeEventReceived endpoint,
	// such as https://example.com/webhooks/calendly.
	CallbackURL string `json:"callbackUrl"`
	// Scope is user or organization; blank uses user. An organization subscription needs an owner or admin.
	Scope string `json:"scope,omitempty"`
	// Events lists invitee.created and invitee.canceled; blank subscribes to both.
	Events []string `json:"events,omitempty"`
	// UserURI is the user of a user subscription; blank uses the connected user.
	UserURI string `json:"userUri,omitempty"`
	// OrganizationURI owns the subscription; blank uses the connected user's current organization.
	OrganizationURI string `json:"organizationUri,omitempty"`
}

// WebhookSubscription is one Calendly webhook subscription.
type WebhookSubscription struct {
	// URI is the subscription's Calendly URI, used to delete it.
	URI string `json:"uri"`
	// CallbackURL is the URL Calendly posts events to.
	CallbackURL string `json:"callbackUrl"`
	// State is active or disabled; Calendly disables a subscription after 24 hours of failed deliveries.
	State string `json:"state"`
	// Events are the webhook events the subscription delivers.
	Events []string `json:"events"`
	// Scope is user, organization, or group.
	Scope string `json:"scope"`
	// OrganizationURI is the organization that owns the subscription.
	OrganizationURI string `json:"organizationUri"`
	// UserURI is the user of a user subscription.
	UserURI string `json:"userUri,omitempty"`
	// CreatedAt is when the subscription was created.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// WasCreated is true when this attempt created the subscription, and false when it reused one. A
	// duplicate dispatch of the same Step can report false for the subscription its first attempt created.
	WasCreated bool `json:"wasCreated"`
}

// CreateWebhookSubscriptionOperation implements the createWebhookSubscription Mutation. Build it with
// Client.CreateWebhookSubscription.
type CreateWebhookSubscriptionOperation struct{ client *Client }

type calendlyWebhookSubscription struct {
	URI          string   `json:"uri"`
	CallbackURL  string   `json:"callback_url"`
	State        string   `json:"state"`
	Events       []string `json:"events"`
	Scope        string   `json:"scope"`
	Organization string   `json:"organization"`
	User         *string  `json:"user"`
	CreatedAt    *string  `json:"created_at"`
}

// webhookSubscriptionRequest is the validated input with the connected user's URIs filled in.
type webhookSubscriptionRequest struct {
	callbackURL     string
	scope           string
	events          []string
	userURI         string
	organizationURI string
}

// Definition returns the immutable createWebhookSubscription operation definition.
func (CreateWebhookSubscriptionOperation) Definition() sdkgo.MutationDefinition {
	return CreateWebhookSubscriptionDefinition
}

// IdempotencyKey returns the call ID. Calendly has no idempotency header; the operation lists before it
// creates instead, so repeating it reuses the subscription.
func (CreateWebhookSubscriptionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateWebhookSubscriptionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists the scope's subscriptions and reuses one whose callback URL matches. Otherwise it posts
// POST /webhook_subscriptions; a 409 lists again and reuses the subscription another attempt created. A
// lost answer or a 5xx returns Retry, because the retry's list finds a subscription the lost request made.
// A personal access token connection registers its webhook_signing_key; an OAuth app's key is Calendly's.
func (operation CreateWebhookSubscriptionOperation) Invoke(call sdkgo.Call, input CreateWebhookSubscriptionInput) sdkgo.MutationAttempt[WebhookSubscription] {
	operationID := CreateWebhookSubscriptionDefinition.Operation.OperationID
	client := operation.client
	request, err := input.validate()
	if err != nil {
		return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchDefect, WebhookSubscription{},
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialMutationAttempt(operationID, CreateWebhookSubscriptionBranchDefect, WebhookSubscription{}, err)
	}
	isSigningKeyRegistered := credentials.AuthMethodID == PersonalAccessTokenAuthMethodID
	if isSigningKeyRegistered && credentials.WebhookSigningKey.Reveal() == "" {
		return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchDefect, WebhookSubscription{},
			calendlyFailurePointer(operationID, sdkgo.FailureValidation,
				"set webhook_signing_key on the Calendly connection before creating a subscription, so deliveries can be verified"), sdkgo.Receipt{})
	}
	if request.organizationURI == "" || (request.scope == WebhookSubscriptionScopeUser && request.userURI == "") {
		currentUser, response, err := client.resolveCurrentUser(call, &credentials)
		if attempt, isTerminal := operation.attemptForFailedExchange(response, err); isTerminal {
			return attempt
		}
		if request.organizationURI == "" {
			request.organizationURI = currentUser.organizationURI
		}
		if request.scope == WebhookSubscriptionScopeUser && request.userURI == "" {
			request.userURI = currentUser.userURI
		}
	}
	if attempt, isFound := operation.reuseListedSubscription(call, &credentials, request); isFound {
		return attempt
	}
	body := map[string]any{
		"url": request.callbackURL, "events": request.events, "organization": request.organizationURI, "scope": request.scope,
	}
	if request.scope == WebhookSubscriptionScopeUser {
		body["user"] = request.userURI
	}
	if isSigningKeyRegistered {
		body["signing_key"] = credentials.WebhookSigningKey.Reveal()
	}
	response, err := client.sendCalendlyRequest(call, &credentials, http.MethodPost, "/webhook_subscriptions", nil, body, nil)
	if err == nil && response.statusCode == http.StatusConflict {
		if attempt, isFound := operation.reuseListedSubscription(call, &credentials, request); isFound {
			return attempt
		}
		// Retrying cannot help: the subscription belongs to a scope or user this list does not cover.
		return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchConflictingSubscription, WebhookSubscription{},
			calendlyFailurePointer(operationID, sdkgo.FailureConflict,
				"Calendly already has a subscription for this callback URL in another scope or for another user; delete it or use another URL"),
			client.calendlyReceipt(""))
	}
	if attempt, isTerminal := operation.attemptForFailedExchange(response, err); isTerminal {
		return attempt
	}
	var decoded struct {
		Resource calendlyWebhookSubscription `json:"resource"`
	}
	var subscription WebhookSubscription
	err = decodeCalendlyJSON(response.body, &decoded)
	if err == nil {
		subscription, err = decoded.Resource.convert()
	}
	if err != nil || subscription.CallbackURL != request.callbackURL {
		return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchInvalidResponse, WebhookSubscription{},
			calendlyFailurePointer(operationID, sdkgo.FailureProtocol, errCalendlyResponseMalformed.Error()), client.calendlyReceipt(""))
	}
	subscription.WasCreated = true
	return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchSubscribed, subscription, nil, client.calendlyReceipt(subscription.URI))
}

// reuseListedSubscription returns false only when the complete list lacks the callback URL.
func (operation CreateWebhookSubscriptionOperation) reuseListedSubscription(
	call sdkgo.Call, credentials *Credentials, request webhookSubscriptionRequest,
) (sdkgo.MutationAttempt[WebhookSubscription], bool) {
	operationID := CreateWebhookSubscriptionDefinition.Operation.OperationID
	client := operation.client
	query := url.Values{
		"organization": {request.organizationURI}, "scope": {request.scope},
		"count": {strconv.Itoa(maximumPageSize)}, "sort": {"created_at:asc"},
	}
	if request.scope == WebhookSubscriptionScopeUser {
		query.Set("user", request.userURI)
	}
	for page := 0; page < maximumWebhookSubscriptionPages; page++ {
		response, err := client.sendCalendlyRequest(call, credentials, http.MethodGet, "/webhook_subscriptions", query, nil, nil)
		if attempt, isTerminal := operation.attemptForFailedExchange(response, err); isTerminal {
			return attempt, true
		}
		var decoded struct {
			Collection []calendlyWebhookSubscription `json:"collection"`
			Pagination calendlyPagination            `json:"pagination"`
		}
		var nextPageToken string
		err = decodeCalendlyJSON(response.body, &decoded)
		if err == nil {
			nextPageToken, err = decoded.Pagination.nextPageToken()
		}
		for _, item := range decoded.Collection {
			if err != nil || item.CallbackURL != request.callbackURL {
				continue
			}
			var existing WebhookSubscription
			if existing, err = item.convert(); err == nil {
				return operation.attemptForExistingSubscription(existing, request.events), true
			}
		}
		if err != nil {
			return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchInvalidResponse, WebhookSubscription{},
				calendlyFailurePointer(operationID, sdkgo.FailureProtocol, errCalendlyResponseMalformed.Error()), client.calendlyReceipt("")), true
		}
		if nextPageToken == "" {
			return sdkgo.MutationAttempt[WebhookSubscription]{}, false
		}
		query.Set("page_token", nextPageToken)
	}
	return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchInvalidResponse, WebhookSubscription{},
		calendlyFailurePointer(operationID, sdkgo.FailureResponseTooLarge,
			fmt.Sprintf("the scope has more than %d webhook subscriptions", maximumWebhookSubscriptionPages*maximumPageSize)), client.calendlyReceipt("")), true
}

// attemptForExistingSubscription reuses an active subscription that covers every requested event.
func (operation CreateWebhookSubscriptionOperation) attemptForExistingSubscription(
	existing WebhookSubscription, requestedEvents []string,
) sdkgo.MutationAttempt[WebhookSubscription] {
	client := operation.client
	isCovering := existing.State == WebhookSubscriptionStateActive
	for _, event := range requestedEvents {
		isCovering = isCovering && slices.Contains(existing.Events, event)
	}
	if isCovering {
		return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchSubscribed, existing, nil, client.calendlyReceipt(existing.URI))
	}
	return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchConflictingSubscription, existing,
		calendlyFailurePointer(CreateWebhookSubscriptionDefinition.Operation.OperationID, sdkgo.FailureConflict,
			"a Calendly subscription for this callback URL is disabled or lacks a requested event; delete it and run again"),
		client.calendlyReceipt(existing.URI))
}

// attemptForFailedExchange returns false for a 2xx; a lost answer retries, as the next list finds a create.
func (operation CreateWebhookSubscriptionOperation) attemptForFailedExchange(
	response calendlyResponse, err error,
) (sdkgo.MutationAttempt[WebhookSubscription], bool) {
	operationID := CreateWebhookSubscriptionDefinition.Operation.OperationID
	client := operation.client
	if err != nil {
		switch {
		case errors.Is(err, errCalendlyRequestInvalid):
			return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchDefect, WebhookSubscription{},
				calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{}), true
		case errors.Is(err, errCalendlyResponseTooLarge), errors.Is(err, errCalendlyResponseMalformed):
			return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchInvalidResponse, WebhookSubscription{},
				calendlyFailurePointer(operationID, responseFailureKind(err), responseFailureMessage(err)), client.calendlyReceipt("")), true
		default:
			return sdkgo.NewMutationRetry[WebhookSubscription](calendlyFailure(operationID, sdkgo.FailureTransport,
				"Calendly's answer was lost; the retry lists subscriptions before creating one"), 0), true
		}
	}
	if response.statusCode >= 200 && response.statusCode < 300 {
		return sdkgo.MutationAttempt[WebhookSubscription]{}, false
	}
	outcome := client.classifyCalendlyFailure(operationID, response)
	if outcome.isRetry {
		return sdkgo.NewMutationRetry[WebhookSubscription](outcome.failure, outcome.retryAfter), true
	}
	return sdkgo.NewMutationBranch(CreateWebhookSubscriptionBranchProviderRejected, WebhookSubscription{}, &outcome.failure, client.calendlyReceipt("")), true
}

func (input CreateWebhookSubscriptionInput) validate() (webhookSubscriptionRequest, error) {
	callbackURL, err := url.Parse(input.CallbackURL)
	if err != nil || callbackURL.Scheme != "https" || callbackURL.Hostname() == "" || callbackURL.User != nil || callbackURL.Fragment != "" {
		return webhookSubscriptionRequest{}, fmt.Errorf("callbackUrl must be an absolute HTTPS URL without user information or a fragment")
	}
	request := webhookSubscriptionRequest{
		callbackURL: input.CallbackURL, scope: input.Scope, userURI: input.UserURI, organizationURI: input.OrganizationURI,
	}
	switch request.scope {
	case "":
		request.scope = WebhookSubscriptionScopeUser
	case WebhookSubscriptionScopeUser, WebhookSubscriptionScopeOrganization:
	default:
		return webhookSubscriptionRequest{}, fmt.Errorf("scope must be user, organization, or blank")
	}
	if request.scope == WebhookSubscriptionScopeOrganization && request.userURI != "" {
		return webhookSubscriptionRequest{}, fmt.Errorf("userUri applies only to a user subscription")
	}
	if request.userURI != "" {
		if _, err := parseUserURI(request.userURI); err != nil {
			return webhookSubscriptionRequest{}, err
		}
	}
	if request.organizationURI != "" {
		if _, err := parseOrganizationURI(request.organizationURI); err != nil {
			return webhookSubscriptionRequest{}, err
		}
	}
	request.events = input.Events
	if len(request.events) == 0 {
		request.events = []string{WebhookEventInviteeCreated, WebhookEventInviteeCanceled}
	}
	if !areSupportedWebhookEvents(request.events) {
		return webhookSubscriptionRequest{}, fmt.Errorf("events must be distinct values from invitee.created and invitee.canceled")
	}
	return request, nil
}

// areSupportedWebhookEvents reports whether events holds distinct invitee events this connector decodes.
func areSupportedWebhookEvents(events []string) bool {
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		if (event != WebhookEventInviteeCreated && event != WebhookEventInviteeCanceled) || seen[event] {
			return false
		}
		seen[event] = true
	}
	return true
}

func (subscription calendlyWebhookSubscription) convert() (WebhookSubscription, error) {
	if _, err := parseCalendlyResourceURI(subscription.URI, "/webhook_subscriptions/", "webhook subscription"); err != nil {
		return WebhookSubscription{}, errCalendlyResponseMalformed
	}
	if subscription.CallbackURL == "" || subscription.State == "" || subscription.Scope == "" {
		return WebhookSubscription{}, errCalendlyResponseMalformed
	}
	createdAt, err := parseOptionalCalendlyTimestamp(subscription.CreatedAt)
	if err != nil {
		return WebhookSubscription{}, err
	}
	return WebhookSubscription{
		URI: subscription.URI, CallbackURL: subscription.CallbackURL, State: subscription.State,
		Events: subscription.Events, Scope: subscription.Scope, OrganizationURI: subscription.Organization,
		UserURI: stringValue(subscription.User), CreatedAt: createdAt,
	}, nil
}
