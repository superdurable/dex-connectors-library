// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// typeformSignatureHeader carries sha256= and the base64 HMAC-SHA256 of the raw body.
	typeformSignatureHeader = "Typeform-Signature"
	typeformSignaturePrefix = "sha256="
)

var errTypeformSignatureInvalid = errors.New("Typeform-Signature is missing, malformed, or does not match")

// ResponseSubmittedTriggerConfiguration filters the submissions one binding records. The zero value
// records every form's submissions. It reduces what a binding stores; the application's TriggerFilter
// remains its admission rule.
type ResponseSubmittedTriggerConfiguration struct {
	// FormID accepts only submissions of this form, such as the formPicker unit's formId; blank accepts
	// every form whose webhook points at the endpoint.
	FormID string `json:"formId,omitempty"`
}

// FormResponseEvent is one verified form_response webhook. The Trigger event ID is the form ID and the
// response token, such as lT4Z3j:a3a12ec67a1365927098a606107fac15, so every redelivery of one submission,
// and a copy sent by a second webhook of the same form, keeps one ID.
type FormResponseEvent struct {
	// WebhookEventID is Typeform's event_id for this delivery; it is not the Trigger event ID.
	WebhookEventID string `json:"webhookEventId"`
	// FormID is the submitted form's ID.
	FormID string `json:"formId"`
	// FormTitle is the form's title from the delivery's form definition.
	FormTitle string `json:"formTitle,omitempty"`
	// Response is the submission, with each answer's field ref, type, title, and typed value, and the
	// hidden fields.
	Response FormResponse `json:"response"`
}

type typeformWebhookEnvelope struct {
	EventID      string                       `json:"event_id"`
	EventType    string                       `json:"event_type"`
	FormResponse *typeformWebhookFormResponse `json:"form_response"`
}

type typeformWebhookFormResponse struct {
	typeformResponseFields
	FormID     string `json:"form_id"`
	Definition *struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Fields []struct {
			ID    string `json:"id"`
			Ref   string `json:"ref"`
			Title string `json:"title"`
		} `json:"fields"`
	} `json:"definition"`
}

// Validate checks that FormID is blank or a Typeform form ID.
func (configuration ResponseSubmittedTriggerConfiguration) Validate() error {
	if configuration.FormID == "" {
		return nil
	}
	if err := validateFormID(configuration.FormID); err != nil {
		return fmt.Errorf("responseSubmitted %w", err)
	}
	return nil
}

// acceptsEvent applies the binding's form filter.
func (configuration ResponseSubmittedTriggerConfiguration) acceptsEvent(event sdkgo.TriggerEvent[FormResponseEvent]) bool {
	return configuration.FormID == "" || configuration.FormID == event.Payload.FormID
}

// ResponseSubmittedWebhookHandler returns the connection's webhook endpoint for an application to mount at
// the public HTTPS URL of its Typeform webhooks. Every responseSubmitted Trigger built from this Connection
// feeds from it, and it answers 503 while none of them runs, so Typeform retries.
func (connection Connection) ResponseSubmittedWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.responseSubmittedWebhookEndpoint(connection.reference)
}

func (client *Client) responseSubmittedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration ResponseSubmittedTriggerConfiguration,
) sdkgo.TriggerSource[FormResponseEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.responseSubmittedWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(configuration.acceptsEvent)
}

// responseSubmittedWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) responseSubmittedWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, FormResponseEvent], error) {
	client.responseSubmittedEndpointsMu.Lock()
	defer client.responseSubmittedEndpointsMu.Unlock()
	if endpoint, isFound := client.responseSubmittedEndpoints[connection]; isFound {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, FormResponseEvent]{
		ConnectorID: ConnectorID, TriggerName: ResponseSubmittedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, MaxBodyBytes: client.webhookMaxBodyBytes,
		VerifyRequest: verifyResponseSubmittedRequest, DecodeEvent: decodeResponseSubmittedRequest,
		Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("Typeform webhook endpoint: %w", err)
	}
	client.responseSubmittedEndpoints[connection] = endpoint
	return endpoint, nil
}

// verifyResponseSubmittedRequest answers 503 without a secret, so Typeform keeps retrying the delivery.
func verifyResponseSubmittedRequest(request webhooktrigger.Request, credentials Credentials) error {
	secret := credentials.WebhookSecret.Reveal()
	if secret == "" {
		return webhooktrigger.ErrVerificationUnavailable
	}
	return verifyTypeformSignature(request.Body, request.Header.Get(typeformSignatureHeader), secret)
}

// verifyTypeformSignature requires sha256= and the base64 HMAC-SHA256 of the raw body, compared in constant time.
func verifyTypeformSignature(body []byte, header string, secret string) error {
	encoded, hasPrefix := strings.CutPrefix(strings.TrimSpace(header), typeformSignaturePrefix)
	if !hasPrefix {
		return errTypeformSignatureInvalid
	}
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(signature) != sha256.Size {
		return errTypeformSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body) // hash.Hash writes never fail.
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errTypeformSignatureInvalid
	}
	return nil
}

// decodeResponseSubmittedRequest decodes a verified delivery. Another event type, such as
// form_response_partial, is acknowledged without a record.
func decodeResponseSubmittedRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[FormResponseEvent], bool, error) {
	var envelope typeformWebhookEnvelope
	if err := json.Unmarshal(request.Body, &envelope); err != nil || envelope.EventType == "" {
		return sdkgo.TriggerEvent[FormResponseEvent]{}, false, errTypeformResponseMalformed
	}
	if envelope.EventType != WebhookEventTypeFormResponse {
		return sdkgo.TriggerEvent[FormResponseEvent]{}, false, nil
	}
	submission := envelope.FormResponse
	if submission == nil || !typeformIDPattern.MatchString(submission.FormID) {
		return sdkgo.TriggerEvent[FormResponseEvent]{}, false, errTypeformResponseMalformed
	}
	definitions := map[string]typeformFieldDefinition{}
	formTitle := ""
	if submission.Definition != nil {
		if submission.Definition.ID != "" && submission.Definition.ID != submission.FormID {
			return sdkgo.TriggerEvent[FormResponseEvent]{}, false, errTypeformResponseMalformed
		}
		formTitle = submission.Definition.Title
		for _, field := range submission.Definition.Fields {
			definitions[field.ID] = typeformFieldDefinition{ref: field.Ref, title: field.Title}
		}
	}
	response, err := submission.convert(definitions)
	if err != nil || response.SubmittedAt.IsZero() {
		return sdkgo.TriggerEvent[FormResponseEvent]{}, false, errTypeformResponseMalformed
	}
	return sdkgo.TriggerEvent[FormResponseEvent]{
		ID: submission.FormID + ":" + response.Token, OccurredAt: response.SubmittedAt,
		Payload: FormResponseEvent{WebhookEventID: envelope.EventID, FormID: submission.FormID, FormTitle: formTitle, Response: response},
	}, true, nil
}
