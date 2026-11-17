// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateDNSRecordInput supplies a new DNS record and its zone.
type CreateDNSRecordInput struct {
	// ZoneID identifies the zone.
	ZoneID string `json:"zoneId"`
	// Type is A, AAAA, CNAME, or TXT.
	Type string `json:"type"`
	// Name is the full DNS name.
	Name string `json:"name"`
	// Content is the address, target, or text value.
	Content string `json:"content"`
	// TTL is seconds, or one for Cloudflare automatic TTL.
	TTL int `json:"ttl"`
	// Proxied enables Cloudflare proxying for eligible records.
	Proxied bool `json:"proxied"`
}

// CreateDNSRecordOperation creates a DNS record with bounded possible sends.
type CreateDNSRecordOperation struct{ client *Client }

// CreateDNSRecord binds the Mutation to this client.
func (client *Client) CreateDNSRecord() CreateDNSRecordOperation {
	return CreateDNSRecordOperation{client}
}

// Definition returns the generated createDNSRecord contract.
func (CreateDNSRecordOperation) Definition() sdkgo.MutationDefinition {
	return CreateDNSRecordDefinition
}

// IdempotencyKey correlates attempts; Cloudflare does not deduplicate this key.
func (CreateDNSRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDNSRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates the record and reconciles a known provider ID before resending.
func (operation CreateDNSRecordOperation) Invoke(call sdkgo.Call, input CreateDNSRecordInput) sdkgo.MutationAttempt[DNSRecord] {
	if !validCloudflareID(input.ZoneID) {
		return sdkgo.NewMutationBranch(CreateDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureValidation, "createDNSRecord", "zoneId is invalid"), sdkgo.Receipt{})
	}
	if err := validateDNSRecordFields(input.Type, input.Name, input.Content, input.TTL, input.Proxied); err != nil {
		return sdkgo.NewMutationBranch(CreateDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureValidation, "createDNSRecord", err.Error()), sdkgo.Receipt{})
	}
	body, err := json.Marshal(struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		TTL     int    `json:"ttl"`
		Proxied bool   `json:"proxied"`
	}{input.Type, input.Name, input.Content, input.TTL, input.Proxied})
	if err != nil {
		return sdkgo.NewMutationBranch(CreateDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureLocalDefect, "createDNSRecord", "record could not be encoded"), sdkgo.Receipt{})
	}
	return operation.invokeCreate(call, input.ZoneID, body)
}

func (operation CreateDNSRecordOperation) invokeCreate(call sdkgo.Call, zoneID string, body []byte) sdkgo.MutationAttempt[DNSRecord] {
	const operationID = "createDNSRecord"
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureLocalDefect, operationID, "dispatch fence could not be initialized"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return cloudflareCreateUncertain(sdkgo.FailureTransport, "earlier creation cannot be reconciled")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return cloudflareCreateUncertain(sdkgo.FailureAuthentication, "earlier creation may have succeeded; credentials unavailable")
		}
		return sdkgo.NewMutationBranch(CreateDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureAuthentication, operationID, "connection credentials are unavailable"), sdkgo.Receipt{})
	}
	if decision.Marker.ProviderID != "" {
		if !validCloudflareID(decision.Marker.ProviderID) {
			return cloudflareCreateUncertain(sdkgo.FailureProtocol, "saved provider identity is invalid")
		}
		record, found, retry := operation.readBack(call, zoneID, decision.Marker.ProviderID)
		if found {
			return sdkgo.NewMutationBranch(CreateDNSRecordBranchCreated, record, nil, sdkgo.Receipt{})
		}
		if retry {
			return sdkgo.NewMutationRetry[DNSRecord](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "cloudflare", Operation: operationID, Message: "creation read-back unavailable"}, 0)
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return cloudflareCreateUncertain(sdkgo.FailureTransport, "earlier creation cannot be confirmed")
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		request, requestError := http.NewRequestWithContext(requestContext, http.MethodPost, operation.client.baseURL+dnsRecordsPath(zoneID), bytes.NewReader(body))
		if requestError != nil {
			return requestError
		}
		request.Header.Set("Authorization", "Bearer "+credentials.APIToken.Reveal())
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json")
		if requestError := singledispatch.PrepareHTTPWrite(request); requestError != nil {
			return requestError
		}
		response, requestError = operation.client.httpClient.Do(request)
		return requestError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[DNSRecord](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "cloudflare", Operation: operationID, Message: "creation was not dispatched"}, 0)
	}
	if err != nil {
		return cloudflareCreateRetryOrUncertain(sdkgo.FailureTransport, "creation outcome is unknown", canResendUnknown)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[DNSRecord](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "cloudflare", Operation: operationID, Message: "retry checkpoint unavailable"}, 0)
		}
		retryFailure, retryAfter := retryFailure(response, operationID)
		return sdkgo.NewMutationRetry[DNSRecord](retryFailure, retryAfter)
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return cloudflareCreateRetryOrUncertain(sdkgo.FailureAvailability, "creation outcome is unknown", canResendUnknown)
	}
	if response.StatusCode != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return cloudflareCreateUncertain(sdkgo.FailureProviderRejection, "earlier creation may have succeeded")
		}
		providerFailure := statusFailure(response, operationID)
		return sdkgo.NewMutationBranch(CreateDNSRecordBranchProviderRejected, DNSRecord{}, &providerFailure, sdkgo.Receipt{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return cloudflareCreateUncertain(sdkgo.FailureTransport, "creation succeeded but response was incomplete")
	}
	if isCloudflareRejection(responseBody) {
		if decision.MaximumPriorSends > 0 {
			return cloudflareCreateUncertain(sdkgo.FailureProviderRejection, "earlier creation may have succeeded")
		}
		return sdkgo.NewMutationBranch(CreateDNSRecordBranchProviderRejected, DNSRecord{}, failure(sdkgo.FailureProviderRejection, operationID, "Cloudflare rejected the request"), sdkgo.Receipt{})
	}
	record, err := decodeDNSRecord(responseBody)
	if err != nil {
		return cloudflareCreateUncertain(sdkgo.FailureProtocol, "creation succeeded but response identity is invalid")
	}
	// The response already confirms creation; a failed read-back checkpoint cannot undo it.
	_ = fence.RecordProviderID(record.ID)
	return sdkgo.NewMutationBranch(CreateDNSRecordBranchCreated, record, nil, sdkgo.Receipt{})
}

func (operation CreateDNSRecordOperation) readBack(call sdkgo.Call, zoneID, recordID string) (DNSRecord, bool, bool) {
	response, localFailure, err := operation.client.send(call, "createDNSRecord", requestSpec{method: http.MethodGet, path: dnsRecordPath(zoneID, recordID)})
	if localFailure != nil || err != nil {
		return DNSRecord{}, false, true
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return DNSRecord{}, false, false
	}
	if response.StatusCode != http.StatusOK {
		return DNSRecord{}, false, true
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return DNSRecord{}, false, true
	}
	record, err := decodeDNSRecord(body)
	return record, err == nil, err != nil
}

func cloudflareCreateRetryOrUncertain(kind sdkgo.FailureKind, message string, canResend bool) sdkgo.MutationAttempt[DNSRecord] {
	if canResend {
		return sdkgo.NewMutationRetry[DNSRecord](sdkgo.Failure{Kind: kind, Provider: "cloudflare", Operation: "createDNSRecord", Message: message}, 0)
	}
	return cloudflareCreateUncertain(kind, message)
}

func cloudflareCreateUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DNSRecord] {
	return sdkgo.NewMutationUncertain(DNSRecord{}, sdkgo.Failure{Kind: kind, Provider: "cloudflare", Operation: "createDNSRecord", Message: message}, sdkgo.Receipt{})
}
