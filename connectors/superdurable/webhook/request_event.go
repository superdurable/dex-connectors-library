// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// Accepted request media types.
const (
	// ContentTypeJSON is a JSON body, kept unchanged in WebhookRequestEvent.JSONBody.
	ContentTypeJSON = "application/json"
	// ContentTypeForm is a URL-encoded form body, decoded into WebhookRequestEvent.FormBody.
	ContentTypeForm = "application/x-www-form-urlencoded"
)

// maxEventIDBytes bounds event IDs, which applications embed in Flow IDs.
const maxEventIDBytes = 256

// forwardingDeniedHeaders carry credentials or signatures and never enter an event.
var forwardingDeniedHeaders = []string{
	"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie",
	textproto.CanonicalMIMEHeaderKey(standardWebhooksSignatureHeader),
}

// WebhookRequestEvent is one verified webhook request. The Trigger event's ID is the request's event
// ID: webhook-id for Standard Webhooks, otherwise eventIdHeader, then eventIdPointer, then the hex
// SHA-256 of the body. Exactly one of JSONBody and FormBody is set.
type WebhookRequestEvent struct {
	// ContentType is the request media type without parameters: ContentTypeJSON or ContentTypeForm.
	ContentType string `json:"contentType"`
	// Headers holds the configured forwardedHeaders present on the request, keyed by canonical name.
	// Repeated values are joined with ", ". It never holds a credential or signature header.
	Headers map[string]string `json:"headers,omitempty"`
	// JSONBody is the unchanged body of an application/json request.
	JSONBody json.RawMessage `json:"jsonBody,omitempty"`
	// FormBody is the decoded body of an application/x-www-form-urlencoded request.
	FormBody map[string][]string `json:"formBody,omitempty"`
	// ReceivedAt is when the endpoint read the request, in UTC. It is also the Trigger event's OccurredAt.
	ReceivedAt time.Time `json:"receivedAt"`
}

// RequestReceivedTriggerConfiguration filters the events one requestReceived binding accepts. An empty
// configuration accepts every verified event. An event the binding rejects is still answered 200.
// NewRequestReceivedTrigger panics for an invalid configuration, so call Validate on untrusted values.
type RequestReceivedTriggerConfiguration struct {
	// MatchPointer is an RFC 6901 JSON Pointer into the event body, such as /event_type; in a form body,
	// /field/0 names the first value of field. Empty accepts every event.
	MatchPointer string `json:"matchPointer,omitempty"`
	// MatchValues lists the accepted values at MatchPointer. A string matches its text, and a number or
	// boolean matches its literal JSON text, such as 42 or true. It is required with MatchPointer.
	MatchValues []string `json:"matchValues,omitempty"`
}

// Validate checks that MatchPointer is a valid JSON Pointer and that it and MatchValues are set together.
func (configuration RequestReceivedTriggerConfiguration) Validate() error {
	_, err := newRequestMatcher(configuration)
	return err
}

// requestMatcher applies one binding's configuration; a zero pointer accepts every event.
type requestMatcher struct {
	pointer       jsonPointer
	hasPointer    bool
	matchedValues []string
}

func newRequestMatcher(configuration RequestReceivedTriggerConfiguration) (requestMatcher, error) {
	switch {
	case configuration.MatchPointer == "" && len(configuration.MatchValues) == 0:
		return requestMatcher{}, nil
	case configuration.MatchPointer == "" || len(configuration.MatchValues) == 0:
		return requestMatcher{}, errors.New("webhook requestReceived matchPointer and matchValues must be set together")
	}
	pointer, err := parseJSONPointer(configuration.MatchPointer)
	if err != nil {
		return requestMatcher{}, fmt.Errorf("webhook requestReceived matchPointer: %w", err)
	}
	return requestMatcher{pointer: pointer, hasPointer: true, matchedValues: configuration.MatchValues}, nil
}

func (matcher requestMatcher) acceptsEvent(event sdkgo.TriggerEvent[WebhookRequestEvent]) bool {
	if !matcher.hasPointer {
		return true
	}
	document, err := event.Payload.bodyDocument()
	if err != nil {
		return false
	}
	value, isFound := matcher.pointer.resolve(document)
	if !isFound {
		return false
	}
	text, isScalar := scalarText(value)
	return isScalar && slices.Contains(matcher.matchedValues, text)
}

// requestEventDecoder turns a verified request into an event with a stable ID.
type requestEventDecoder struct {
	scheme            VerificationScheme
	eventIDHeader     string
	eventIDPointer    jsonPointer
	hasEventIDPointer bool
	forwardedHeaders  []string
}

func newRequestEventDecoder(config *Config) (requestEventDecoder, error) {
	decoder := requestEventDecoder{scheme: config.VerificationScheme}
	if config.EventIDHeader != "" {
		eventIDHeader, err := canonicalHeaderName("eventIdHeader", config.EventIDHeader)
		if err != nil {
			return requestEventDecoder{}, err
		}
		decoder.eventIDHeader = eventIDHeader
	}
	if config.EventIDPointer != "" {
		pointer, err := parseJSONPointer(config.EventIDPointer)
		if err != nil {
			return requestEventDecoder{}, fmt.Errorf("webhook eventIdPointer: %w", err)
		}
		decoder.eventIDPointer, decoder.hasEventIDPointer = pointer, true
	}
	deniedHeaders := append(slices.Clone(forwardingDeniedHeaders),
		textproto.CanonicalMIMEHeaderKey(config.SignatureHeader), textproto.CanonicalMIMEHeaderKey(config.TokenHeader))
	for _, name := range config.ForwardedHeaders {
		forwardedHeader, err := canonicalHeaderName("forwardedHeaders entry", name)
		if err != nil {
			return requestEventDecoder{}, err
		}
		if slices.Contains(deniedHeaders, forwardedHeader) {
			return requestEventDecoder{}, fmt.Errorf("webhook forwardedHeaders cannot include %s", forwardedHeader)
		}
		if !slices.Contains(decoder.forwardedHeaders, forwardedHeader) {
			decoder.forwardedHeaders = append(decoder.forwardedHeaders, forwardedHeader)
		}
	}
	return decoder, nil
}

// decodeEvent handles every verified request; an unsupported body or a missing event ID answers 400.
func (decoder requestEventDecoder) decodeEvent(request webhooktrigger.Request) (sdkgo.TriggerEvent[WebhookRequestEvent], bool, error) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return sdkgo.TriggerEvent[WebhookRequestEvent]{}, false, errors.New("webhook request has no valid Content-Type")
	}
	event := WebhookRequestEvent{
		ContentType: mediaType, Headers: decoder.forwardedHeaderValues(request.Header), ReceivedAt: request.ReceivedAt.UTC(),
	}
	switch mediaType {
	case ContentTypeJSON:
		if !json.Valid(request.Body) {
			return sdkgo.TriggerEvent[WebhookRequestEvent]{}, false, errors.New("webhook JSON body is invalid")
		}
		event.JSONBody = request.Body
	case ContentTypeForm:
		values, err := url.ParseQuery(string(request.Body))
		if err != nil {
			return sdkgo.TriggerEvent[WebhookRequestEvent]{}, false, errors.New("webhook form body is invalid")
		}
		event.FormBody = values
	default:
		return sdkgo.TriggerEvent[WebhookRequestEvent]{}, false, fmt.Errorf("webhook Content-Type %s is not supported", mediaType)
	}
	eventID, err := decoder.resolveEventID(request, event)
	if err != nil {
		return sdkgo.TriggerEvent[WebhookRequestEvent]{}, false, err
	}
	return sdkgo.TriggerEvent[WebhookRequestEvent]{ID: eventID, OccurredAt: event.ReceivedAt, Payload: event}, true, nil
}

// resolveEventID falls back to the body digest only when no configured source applies.
func (decoder requestEventDecoder) resolveEventID(request webhooktrigger.Request, event WebhookRequestEvent) (string, error) {
	switch {
	case decoder.scheme == VerificationSchemeStandardWebhooks:
		return validateEventID(request.Header.Get(standardWebhooksIDHeader))
	case decoder.eventIDHeader != "" && request.Header.Get(decoder.eventIDHeader) != "":
		return validateEventID(request.Header.Get(decoder.eventIDHeader))
	case decoder.hasEventIDPointer:
		document, err := event.bodyDocument()
		if err != nil {
			return "", err
		}
		value, isFound := decoder.eventIDPointer.resolve(document)
		text, isScalar := scalarText(value)
		if !isFound || !isScalar {
			return "", errors.New("webhook eventIdPointer does not name a string or number")
		}
		return validateEventID(text)
	default:
		digest := sha256.Sum256(request.Body)
		return hex.EncodeToString(digest[:]), nil
	}
}

func (decoder requestEventDecoder) forwardedHeaderValues(header http.Header) map[string]string {
	var values map[string]string
	for _, name := range decoder.forwardedHeaders {
		headerValues := header.Values(name)
		if len(headerValues) == 0 {
			continue
		}
		if values == nil {
			values = make(map[string]string, len(decoder.forwardedHeaders))
		}
		values[name] = strings.Join(headerValues, ", ")
	}
	return values
}

// bodyDocument returns the decoded JSON body, or the form body as an object of string arrays.
func (event WebhookRequestEvent) bodyDocument() (any, error) {
	if event.JSONBody == nil {
		document := make(map[string]any, len(event.FormBody))
		for name, values := range event.FormBody {
			items := make([]any, len(values))
			for index, value := range values {
				items[index] = value
			}
			document[name] = items
		}
		return document, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(event.JSONBody))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("webhook JSON body is invalid")
	}
	return document, nil
}

// validateEventID accepts 1 to 256 printable ASCII characters without spaces.
func validateEventID(eventID string) (string, error) {
	if eventID == "" || len(eventID) > maxEventIDBytes {
		return "", fmt.Errorf("webhook event ID must be 1 to %d characters", maxEventIDBytes)
	}
	for index := 0; index < len(eventID); index++ {
		if eventID[index] < 0x21 || eventID[index] > 0x7e {
			return "", errors.New("webhook event ID must be printable ASCII without spaces")
		}
	}
	return eventID, nil
}

// scalarText returns a string's text or a number's or boolean's literal JSON text.
func scalarText(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case json.Number:
		return typed.String(), true
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}
