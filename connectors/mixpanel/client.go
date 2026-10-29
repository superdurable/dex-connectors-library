// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mixpanel imports deduplicated events and exports bounded raw events.
package mixpanel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures a client dependency at construction.
type Option interface{ applyClientOption(*Client) }

// Client holds immutable HTTP settings and resolves service-account credentials for each call.
type Client struct {
	baseURL          string
	exportBaseURL    string
	projectID        int64
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// TrackEventInput identifies one stable event across Dex retries.
type TrackEventInput struct {
	// Event is the Mixpanel event name.
	Event string `json:"event"`
	// DistinctID identifies the associated user and must remain stable across retries.
	DistinctID string `json:"distinctID"`
	// Time is the event's Unix timestamp in seconds and must remain stable across retries.
	Time int64 `json:"time"`
	// Properties contains optional non-secret string properties.
	Properties map[string]string `json:"properties,omitempty"`
}

// EventImportResult confirms Mixpanel accepted one event for ingestion.
type EventImportResult struct {
	// Imported means the strict import response accepted the single event.
	Imported bool `json:"imported"`
}

// ExportEventsInput selects a bounded raw event export.
type ExportEventsInput struct {
	// FromDate is the inclusive YYYY-MM-DD start date.
	FromDate string `json:"fromDate"`
	// ToDate is the inclusive YYYY-MM-DD end date.
	ToDate string `json:"toDate"`
	// Limit is the maximum number of raw events, from 1 through 100.
	Limit int `json:"limit"`
}

// ExportedEvent contains only non-secret identity fields from one raw event.
type ExportedEvent struct {
	// Event is the event name.
	Event string `json:"event"`
	// DistinctID identifies the user.
	DistinctID string `json:"distinctID"`
	// Time is the Unix event timestamp in seconds.
	Time int64 `json:"time"`
	// InsertID is the provider's deduplication ID.
	InsertID string `json:"insertID"`
}

// ExportedEvents is a bounded raw event batch; raw exports can include duplicates.
type ExportedEvents struct {
	// Events contains at most the requested limit, without arbitrary properties.
	Events []ExportedEvent `json:"events"`
}

// TrackEventOperation imports one event with a stable deduplication tuple.
type TrackEventOperation struct{ client *Client }

// ExportEventsOperation downloads bounded raw event data.
type ExportEventsOperation struct{ client *Client }

// New validates settings and constructs a reusable Mixpanel client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Mixpanel credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.ProjectID <= 0 {
		return nil, fmt.Errorf("Mixpanel project ID must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Mixpanel ingestion base URL: %w", err)
	}
	exportBaseURL, err := providerhttp.ValidateBaseURL(config.ExportBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Mixpanel export base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, exportBaseURL: exportBaseURL, projectID: config.ProjectID, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// TrackEvent binds event import to this client.
func (client *Client) TrackEvent() TrackEventOperation { return TrackEventOperation{client} }

// ExportEvents binds raw export to this client.
func (client *Client) ExportEvents() ExportEventsOperation { return ExportEventsOperation{client} }

// Definition returns the generated event import contract.
func (TrackEventOperation) Definition() sdkgo.MutationDefinition { return TrackEventDefinition }

// Definition returns the generated raw export contract.
func (ExportEventsOperation) Definition() sdkgo.QueryDefinition { return ExportEventsDefinition }

// IdempotencyKey derives a UUID-shaped $insert_id of 36 provider-accepted bytes from Dex's call ID.
func (TrackEventOperation) IdempotencyKey(callID sdkgo.CallID, _ TrackEventInput) sdkgo.IdempotencyKey {
	digest := sha256.Sum256([]byte(callID))
	part := hex.EncodeToString(digest[:16])
	return sdkgo.IdempotencyKey(part[:8] + "-" + part[8:12] + "-" + part[12:16] + "-" + part[16:20] + "-" + part[20:])
}

// Invoke sends one event with stable event, time, distinct_id, and $insert_id.
func (operation TrackEventOperation) Invoke(call sdkgo.Call, input TrackEventInput) sdkgo.MutationAttempt[EventImportResult] {
	const name = "trackEvent"
	if strings.TrimSpace(input.Event) == "" || strings.TrimSpace(input.DistinctID) == "" || input.Time <= 0 || len(call.IdempotencyKey) != 36 {
		return sdkgo.NewMutationBranch(TrackEventBranchDefect, EventImportResult{}, mixpanelFailure(name, sdkgo.FailureValidation, "event, distinctID, time, and stable call identity are required"), sdkgo.Receipt{})
	}
	properties := map[string]any{"time": input.Time, "distinct_id": input.DistinctID, "$insert_id": string(call.IdempotencyKey), "ip": 0}
	for key, value := range input.Properties {
		if key == "time" || key == "distinct_id" || key == "$insert_id" || key == "ip" || strings.TrimSpace(key) == "" {
			return sdkgo.NewMutationBranch(TrackEventBranchDefect, EventImportResult{}, mixpanelFailure(name, sdkgo.FailureValidation, "event properties include a reserved or blank name"), sdkgo.Receipt{})
		}
		properties[key] = value
	}
	payload, err := json.Marshal([]any{map[string]any{"event": input.Event, "properties": properties}})
	if err != nil || len(payload) >= 1<<20 {
		return sdkgo.NewMutationBranch(TrackEventBranchDefect, EventImportResult{}, mixpanelFailure(name, sdkgo.FailureValidation, "event payload is invalid or too large"), sdkgo.Receipt{})
	}
	query := url.Values{"project_id": {strconv.FormatInt(operation.client.projectID, 10)}, "strict": {"1"}}
	body, status, failure, retryAfter := operation.client.send(call, http.MethodPost, operation.client.baseURL+"/import?"+query.Encode(), payload, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewMutationBranch(TrackEventBranchDefect, EventImportResult{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewMutationRetry[EventImportResult](*failure, retryAfter)
		}
		return sdkgo.NewMutationBranch(TrackEventBranchInvalidResponse, EventImportResult{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewMutationBranch(TrackEventBranchProviderRejected, EventImportResult{}, mixpanelFailure(name, rejectionKind(status), "Mixpanel rejected event import with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var answer struct {
		Code     int    `json:"code"`
		Imported int    `json:"num_records_imported"`
		Status   string `json:"status"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Code != 200 || answer.Imported != 1 || answer.Status != "OK" {
		return sdkgo.NewMutationBranch(TrackEventBranchInvalidResponse, EventImportResult{}, mixpanelFailure(name, sdkgo.FailureProtocol, "Mixpanel returned an invalid import confirmation"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(TrackEventBranchImported, EventImportResult{Imported: true}, nil, sdkgo.Receipt{})
}

// Invoke exports raw JSONL and strips arbitrary event properties from Flow output.
func (operation ExportEventsOperation) Invoke(call sdkgo.Call, input ExportEventsInput) sdkgo.QueryAttempt[ExportedEvents] {
	const name = "exportEvents"
	from, fromError := time.Parse(time.DateOnly, input.FromDate)
	to, toError := time.Parse(time.DateOnly, input.ToDate)
	if fromError != nil || toError != nil || from.After(to) || to.Sub(from) > 30*24*time.Hour || input.Limit < 1 || input.Limit > 100 {
		return sdkgo.NewQueryBranch(ExportEventsBranchDefect, ExportedEvents{}, mixpanelFailure(name, sdkgo.FailureValidation, "date range and limit must be valid"), sdkgo.Receipt{})
	}
	query := url.Values{"project_id": {strconv.FormatInt(operation.client.projectID, 10)}, "from_date": {input.FromDate}, "to_date": {input.ToDate}, "limit": {strconv.Itoa(input.Limit)}}
	body, status, failure, retryAfter := operation.client.send(call, http.MethodGet, operation.client.exportBaseURL+"/export?"+query.Encode(), nil, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(ExportEventsBranchDefect, ExportedEvents{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[ExportedEvents](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(ExportEventsBranchInvalidResponse, ExportedEvents{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(ExportEventsBranchProviderRejected, ExportedEvents{}, mixpanelFailure(name, rejectionKind(status), "Mixpanel rejected raw export with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	result := ExportedEvents{Events: []ExportedEvent{}}
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var wire struct {
			Event      string `json:"event"`
			Properties struct {
				DistinctID string `json:"distinct_id"`
				Time       int64  `json:"time"`
				InsertID   string `json:"$insert_id"`
			} `json:"properties"`
		}
		if json.Unmarshal(line, &wire) != nil || wire.Event == "" {
			return sdkgo.NewQueryBranch(ExportEventsBranchInvalidResponse, ExportedEvents{}, mixpanelFailure(name, sdkgo.FailureProtocol, "Mixpanel returned invalid JSONL"), sdkgo.Receipt{})
		}
		result.Events = append(result.Events, ExportedEvent{Event: wire.Event, DistinctID: wire.Properties.DistinctID, Time: wire.Properties.Time, InsertID: wire.Properties.InsertID})
		if len(result.Events) > input.Limit {
			return sdkgo.NewQueryBranch(ExportEventsBranchInvalidResponse, ExportedEvents{}, mixpanelFailure(name, sdkgo.FailureProtocol, "Mixpanel returned more events than requested"), sdkgo.Receipt{})
		}
	}
	return sdkgo.NewQueryBranch(ExportEventsBranchExported, result, nil, sdkgo.Receipt{})
}

func (client *Client) send(call sdkgo.Call, method, address string, payload []byte, operation string) ([]byte, int, *sdkgo.Failure, time.Duration) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.ServiceAccountUsername.Reveal()) || !providerhttp.IsHeaderSafeCredential(credentials.ServiceAccountSecret.Reveal()) {
		return nil, 0, mixpanelFailure(operation, sdkgo.FailureAuthentication, "Mixpanel credentials are unavailable or invalid"), 0
	}
	request, err := http.NewRequestWithContext(call.Context, method, address, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, mixpanelFailure(operation, sdkgo.FailureLocalDefect, "Mixpanel request could not be built"), 0
	}
	request.SetBasicAuth(credentials.ServiceAccountUsername.Reveal(), credentials.ServiceAccountSecret.Reveal())
	request.Header.Set("Accept", "application/json")
	if method == http.MethodGet {
		request.Header.Set("Accept", "text/plain")
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, -1, mixpanelFailure(operation, sdkgo.FailureTransport, "Mixpanel response is unavailable"), 0
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, -1, mixpanelFailure(operation, sdkgo.FailureAvailability, "Mixpanel is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, nil, 0
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return nil, 1, mixpanelFailure(operation, sdkgo.FailureResponseTooLarge, "Mixpanel response exceeded the limit"), 0
	}
	if err != nil {
		return nil, -1, mixpanelFailure(operation, sdkgo.FailureAvailability, "Mixpanel response was interrupted"), 0
	}
	return body, response.StatusCode, nil, 0
}

func rejectionKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}
func mixpanelFailure(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "mixpanel", Operation: operation, Message: message}
}
