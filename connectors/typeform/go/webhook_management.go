// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

import (
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListWebhooksInput identifies a form whose webhooks should be listed.
type ListWebhooksInput struct {
	// FormID is the form ID from its Typeform URL.
	FormID string `json:"formId"`
}

// WebhookList contains the form's webhooks without their signing secrets.
type WebhookList struct {
	// Webhooks contains the stored webhooks.
	Webhooks []Webhook `json:"webhooks"`
}

// GetWebhookInput identifies one form webhook by its tag.
type GetWebhookInput struct {
	// FormID is the form ID from its Typeform URL.
	FormID string `json:"formId"`
	// Tag identifies the webhook within the form.
	Tag string `json:"tag"`
}

// DeleteWebhookInput identifies one form webhook to remove.
type DeleteWebhookInput struct {
	// FormID is the form ID from its Typeform URL.
	FormID string `json:"formId"`
	// Tag identifies the webhook within the form.
	Tag string `json:"tag"`
}

// DeletedWebhook identifies a webhook removed or already absent.
type DeletedWebhook struct {
	// FormID is the form ID from its Typeform URL.
	FormID string `json:"formId"`
	// Tag identifies the webhook within the form.
	Tag string `json:"tag"`
}

// ListWebhooksOperation reads a form's webhooks.
type ListWebhooksOperation struct{ client *Client }

// GetWebhookOperation reads one form webhook.
type GetWebhookOperation struct{ client *Client }

// DeleteWebhookOperation removes a form webhook.
type DeleteWebhookOperation struct{ client *Client }

// ListWebhooks binds the listWebhooks Query to this client.
func (client *Client) ListWebhooks() ListWebhooksOperation { return ListWebhooksOperation{client} }

// GetWebhook binds the getWebhook Query to this client.
func (client *Client) GetWebhook() GetWebhookOperation { return GetWebhookOperation{client} }

// DeleteWebhook binds the deleteWebhook Mutation to this client.
func (client *Client) DeleteWebhook() DeleteWebhookOperation { return DeleteWebhookOperation{client} }

// Definition returns the generated listWebhooks contract.
func (ListWebhooksOperation) Definition() sdkgo.QueryDefinition { return ListWebhooksDefinition }

// Invoke reads and validates the form's webhook list without exposing signing secrets.
func (operation ListWebhooksOperation) Invoke(call sdkgo.Call, input ListWebhooksInput) sdkgo.QueryAttempt[WebhookList] {
	client := operation.client
	operationID := ListWebhooksDefinition.Operation.OperationID
	if err := validateFormID(input.FormID); err != nil {
		return queryAttempt[WebhookList](client, validationOutcome(operationID, err), "")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return queryAttempt[WebhookList](client, credentialOutcome(operationID, err), "")
	}
	response, err := client.sendTypeformRequest(call.Context, credentials, http.MethodGet, "/forms/"+input.FormID+"/webhooks", nil, nil)
	branches := failureBranches{notFound: ListWebhooksBranchNotFound, providerRejected: ListWebhooksBranchProviderRejected, invalidResponse: ListWebhooksBranchInvalidResponse}
	if outcome, terminal := classifyExchange(client, operationID, response, err, branches); terminal {
		return queryAttempt[WebhookList](client, outcome, input.FormID)
	}
	var wire struct {
		Items []typeformWebhook `json:"items"`
	}
	if err := decodeTypeformJSON(response.body, &wire); err != nil || wire.Items == nil {
		return queryAttempt[WebhookList](client, malformedOutcome(operationID, ListWebhooksBranchInvalidResponse), input.FormID)
	}
	result := WebhookList{Webhooks: make([]Webhook, 0, len(wire.Items))}
	for _, item := range wire.Items {
		webhook, err := item.convert()
		if err != nil || webhook.FormID != input.FormID {
			return queryAttempt[WebhookList](client, malformedOutcome(operationID, ListWebhooksBranchInvalidResponse), input.FormID)
		}
		result.Webhooks = append(result.Webhooks, webhook)
	}
	return sdkgo.NewQueryBranch(ListWebhooksBranchListed, result, nil, client.typeformReceipt(input.FormID))
}

// Definition returns the generated getWebhook contract.
func (GetWebhookOperation) Definition() sdkgo.QueryDefinition { return GetWebhookDefinition }

// Invoke reads and validates a webhook without exposing its signing secret.
func (operation GetWebhookOperation) Invoke(call sdkgo.Call, input GetWebhookInput) sdkgo.QueryAttempt[Webhook] {
	client := operation.client
	operationID := GetWebhookDefinition.Operation.OperationID
	if err := validateWebhookIdentity(input.FormID, input.Tag); err != nil {
		return queryAttempt[Webhook](client, validationOutcome(operationID, err), "")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return queryAttempt[Webhook](client, credentialOutcome(operationID, err), "")
	}
	response, err := client.sendTypeformRequest(call.Context, credentials, http.MethodGet, "/forms/"+input.FormID+"/webhooks/"+url.PathEscape(input.Tag), nil, nil)
	branches := failureBranches{notFound: GetWebhookBranchNotFound, providerRejected: GetWebhookBranchProviderRejected, invalidResponse: GetWebhookBranchInvalidResponse}
	if outcome, terminal := classifyExchange(client, operationID, response, err, branches); terminal {
		return queryAttempt[Webhook](client, outcome, input.FormID)
	}
	var wire typeformWebhook
	if err := decodeTypeformJSON(response.body, &wire); err != nil {
		return queryAttempt[Webhook](client, malformedOutcome(operationID, GetWebhookBranchInvalidResponse), input.FormID)
	}
	webhook, err := wire.convert()
	if err != nil || webhook.FormID != input.FormID || webhook.Tag != input.Tag {
		return queryAttempt[Webhook](client, malformedOutcome(operationID, GetWebhookBranchInvalidResponse), input.FormID)
	}
	return sdkgo.NewQueryBranch(GetWebhookBranchFound, webhook, nil, client.typeformReceipt(webhook.ID))
}

// Definition returns the generated deleteWebhook contract.
func (DeleteWebhookOperation) Definition() sdkgo.MutationDefinition { return DeleteWebhookDefinition }

// IdempotencyKey records call identity; a repeated delete leaves the webhook absent.
func (DeleteWebhookOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteWebhookInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one webhook, treating an already absent tag as deleted.
func (operation DeleteWebhookOperation) Invoke(call sdkgo.Call, input DeleteWebhookInput) sdkgo.MutationAttempt[DeletedWebhook] {
	client := operation.client
	operationID := DeleteWebhookDefinition.Operation.OperationID
	if err := validateWebhookIdentity(input.FormID, input.Tag); err != nil {
		return mutationAttempt[DeletedWebhook](client, validationOutcome(operationID, err), "")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return mutationAttempt[DeletedWebhook](client, credentialOutcome(operationID, err), "")
	}
	response, err := client.sendTypeformRequest(call.Context, credentials, http.MethodDelete, "/forms/"+input.FormID+"/webhooks/"+url.PathEscape(input.Tag), nil, nil)
	if err == nil && (response.statusCode >= 200 && response.statusCode < 300 || response.statusCode == http.StatusNotFound) {
		return sdkgo.NewMutationBranch(DeleteWebhookBranchDeleted, DeletedWebhook{FormID: input.FormID, Tag: input.Tag}, nil, client.typeformReceipt(input.Tag))
	}
	branches := failureBranches{providerRejected: DeleteWebhookBranchProviderRejected}
	if outcome, terminal := classifyExchange(client, operationID, response, err, branches); terminal {
		return mutationAttempt[DeletedWebhook](client, outcome, input.Tag)
	}
	return sdkgo.NewMutationRetry[DeletedWebhook](typeformFailure(operationID, sdkgo.FailureProtocol, "Typeform returned an unexpected webhook deletion status"), 0)
}

func validateWebhookIdentity(formID, tag string) error {
	if err := validateFormID(formID); err != nil {
		return err
	}
	if !webhookTagPattern.MatchString(tag) {
		return errTypeformRequestInvalid
	}
	return nil
}
