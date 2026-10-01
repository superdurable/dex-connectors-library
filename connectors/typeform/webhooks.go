// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// WebhookEventTypeFormResponse is the Typeform webhook event sent for each completed submission.
	WebhookEventTypeFormResponse = "form_response"

	// maximumWebhookURLBytes bounds the callback URL sent to Typeform.
	maximumWebhookURLBytes = 2048
)

// webhookTagPattern keeps a tag safe as one URL path segment.
var webhookTagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

// UpsertWebhookInput creates or replaces the webhook a form has under Tag.
type UpsertWebhookInput struct {
	// FormID is the form ID, such as the formPicker unit's formId.
	FormID string `json:"formId"`
	// Tag names the webhook within the form, such as dex-response-recorder; the same tag always updates
	// the same webhook.
	Tag string `json:"tag"`
	// URL is the public HTTPS URL where the application mounts the responseSubmitted endpoint, such as
	// https://example.com/webhooks/typeform. Typeform rejects new http URLs.
	URL string `json:"url"`
	// IsDisabled stores the webhook without delivering to it; false enables delivery.
	IsDisabled bool `json:"disabled,omitempty"`
}

// Webhook is one form webhook as Typeform stored it. Its signing secret is never copied.
type Webhook struct {
	// ID is Typeform's webhook ID.
	ID string `json:"id"`
	// FormID is the form the webhook belongs to.
	FormID string `json:"formId"`
	// Tag is the webhook's tag.
	Tag string `json:"tag"`
	// URL is where Typeform delivers.
	URL string `json:"url"`
	// IsEnabled is true when Typeform delivers submissions to URL.
	IsEnabled bool `json:"isEnabled"`
	// EventTypes are the subscribed event types, such as form_response, in sorted order.
	EventTypes []string `json:"eventTypes"`
	// CreatedAt is when the webhook was first created, in UTC.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// UpdatedAt is when the webhook last changed, in UTC.
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// UpsertWebhookOperation implements the upsertWebhook Mutation. Build it with Client.UpsertWebhook.
type UpsertWebhookOperation struct{ client *Client }

type typeformWebhookRequest struct {
	URL        string          `json:"url"`
	Enabled    bool            `json:"enabled"`
	Secret     string          `json:"secret"`
	EventTypes map[string]bool `json:"event_types"`
}

type typeformWebhook struct {
	ID         string          `json:"id"`
	FormID     string          `json:"form_id"`
	Tag        string          `json:"tag"`
	URL        string          `json:"url"`
	Enabled    *bool           `json:"enabled"`
	EventTypes map[string]bool `json:"event_types"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

// Definition returns the immutable upsertWebhook operation definition.
func (UpsertWebhookOperation) Definition() sdkgo.MutationDefinition {
	return UpsertWebhookDefinition
}

// IdempotencyKey returns the call ID. Typeform has no idempotency header; PUT by tag is idempotent itself.
func (UpsertWebhookOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertWebhookInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends PUT /forms/{form_id}/webhooks/{tag} with the URL, enabled state, form_response event, and
// the connection's webhook_secret. Every attempt sends the same body, so a lost answer, a 5xx, or a
// duplicate dispatch returns Retry or repeats the PUT and still leaves one webhook in the requested state.
func (operation UpsertWebhookOperation) Invoke(call sdkgo.Call, input UpsertWebhookInput) sdkgo.MutationAttempt[Webhook] {
	operationID := UpsertWebhookDefinition.Operation.OperationID
	client := operation.client
	if err := input.validate(); err != nil {
		return mutationAttempt[Webhook](client, validationOutcome(operationID, err), "")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return mutationAttempt[Webhook](client, credentialOutcome(operationID, err), "")
	}
	secret := credentials.WebhookSecret.Reveal()
	if secret == "" {
		return mutationAttempt[Webhook](client, validationOutcome(operationID,
			fmt.Errorf("set webhook_secret on the Typeform connection before registering a webhook, so deliveries can be verified")), "")
	}
	body := typeformWebhookRequest{
		URL: input.URL, Enabled: !input.IsDisabled, Secret: secret,
		EventTypes: map[string]bool{WebhookEventTypeFormResponse: true},
	}
	path := "/forms/" + input.FormID + "/webhooks/" + url.PathEscape(input.Tag)
	response, err := client.sendTypeformRequest(call.Context, credentials, http.MethodPut, path, nil, body)
	branches := failureBranches{
		notFound: UpsertWebhookBranchNotFound, providerRejected: UpsertWebhookBranchProviderRejected,
		invalidResponse: UpsertWebhookBranchInvalidResponse,
	}
	if outcome, isTerminal := classifyExchange(client, operationID, response, err, branches); isTerminal {
		return mutationAttempt[Webhook](client, outcome, input.FormID)
	}
	var decoded typeformWebhook
	if err := decodeTypeformJSON(response.body, &decoded); err != nil {
		return mutationAttempt[Webhook](client, malformedOutcome(operationID, UpsertWebhookBranchInvalidResponse), input.FormID)
	}
	webhook, err := decoded.convert()
	if err != nil || webhook.FormID != input.FormID || webhook.Tag != input.Tag || webhook.IsEnabled == input.IsDisabled {
		return mutationAttempt[Webhook](client, malformedOutcome(operationID, UpsertWebhookBranchInvalidResponse), input.FormID)
	}
	return sdkgo.NewMutationBranch(UpsertWebhookBranchUpserted, webhook, nil, client.typeformReceipt(webhook.ID))
}

func (input UpsertWebhookInput) validate() error {
	if err := validateFormID(input.FormID); err != nil {
		return err
	}
	if !webhookTagPattern.MatchString(input.Tag) {
		return fmt.Errorf("tag must be 1 to 100 letters, digits, '.', '_', or '-', starting with a letter or digit")
	}
	callbackURL, err := url.Parse(input.URL)
	if err != nil || len(input.URL) > maximumWebhookURLBytes || callbackURL.Scheme != "https" || callbackURL.Hostname() == "" ||
		callbackURL.User != nil || callbackURL.Fragment != "" {
		return fmt.Errorf("url must be an absolute HTTPS URL of at most %d bytes without user information or a fragment", maximumWebhookURLBytes)
	}
	return nil
}

// convert drops the echoed secret and requires the identity fields this operation checks.
func (webhook typeformWebhook) convert() (Webhook, error) {
	if webhook.ID == "" || webhook.FormID == "" || webhook.Tag == "" || webhook.URL == "" || webhook.Enabled == nil {
		return Webhook{}, errTypeformResponseMalformed
	}
	createdAt, err := parseOptionalTypeformTimestamp(webhook.CreatedAt)
	if err != nil {
		return Webhook{}, err
	}
	updatedAt, err := parseOptionalTypeformTimestamp(webhook.UpdatedAt)
	if err != nil {
		return Webhook{}, err
	}
	eventTypes := []string{}
	for eventType, isSubscribed := range webhook.EventTypes {
		if isSubscribed {
			eventTypes = append(eventTypes, eventType)
		}
	}
	sort.Strings(eventTypes)
	return Webhook{
		ID: webhook.ID, FormID: webhook.FormID, Tag: webhook.Tag, URL: webhook.URL, IsEnabled: *webhook.Enabled,
		EventTypes: eventTypes, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}
