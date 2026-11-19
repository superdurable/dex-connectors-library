// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"net/url"
	"strings"
)

// CreateDomainClaimInput begins ownership verification for a domain.
type CreateDomainClaimInput struct {
	// Name is the DNS domain to claim.
	Name string `json:"name"`
	// Region is a Resend sending region; blank uses the provider default.
	Region string `json:"region,omitempty"`
	// CustomReturnPath is the optional Return-Path subdomain.
	CustomReturnPath string `json:"customReturnPath,omitempty"`
	// OpenTracking controls open tracking when non-nil.
	OpenTracking *bool `json:"openTracking,omitempty"`
	// ClickTracking controls click tracking when non-nil.
	ClickTracking *bool `json:"clickTracking,omitempty"`
	// TrackingSubdomain is the optional tracking DNS label.
	TrackingSubdomain string `json:"trackingSubdomain,omitempty"`
}

// GetDomainClaimInput selects a placeholder domain's latest claim.
type GetDomainClaimInput struct {
	// DomainID is the placeholder domain ID.
	DomainID string `json:"domainId"`
}

// VerifyDomainClaimInput selects a claim for asynchronous DNS verification.
type VerifyDomainClaimInput GetDomainClaimInput

// DomainClaimRecord is the public DNS TXT challenge to publish.
type DomainClaimRecord struct {
	// Type is TXT.
	Type string `json:"type"`
	// Name is the DNS record name.
	Name string `json:"name"`
	// Value is the publicly published DNS challenge value.
	Value string `json:"value"`
	// TTL is the DNS time-to-live suggestion.
	TTL string `json:"ttl"`
}

// DomainClaim describes an ownership claim and public DNS challenge.
type DomainClaim struct {
	// ID is the provider claim ID.
	ID string `json:"id"`
	// Name is the claimed DNS domain.
	Name string `json:"name"`
	// Status is the provider claim status.
	Status string `json:"status"`
	// DomainID is the placeholder domain ID.
	DomainID string `json:"domain_id"`
	// Region is the sending region.
	Region string `json:"region"`
	// Record is the public DNS TXT challenge.
	Record DomainClaimRecord `json:"record"`
}

// CreateDomainClaimOperation starts a bounded domain claim.
type CreateDomainClaimOperation struct{ client *Client }

// GetDomainClaimOperation retrieves a domain claim.
type GetDomainClaimOperation struct{ client *Client }

// VerifyDomainClaimOperation starts DNS verification.
type VerifyDomainClaimOperation struct{ client *Client }

// CreateDomainClaim binds claim creation to this client.
func (client *Client) CreateDomainClaim() CreateDomainClaimOperation {
	return CreateDomainClaimOperation{client}
}

// GetDomainClaim binds claim retrieval to this client.
func (client *Client) GetDomainClaim() GetDomainClaimOperation {
	return GetDomainClaimOperation{client}
}

// VerifyDomainClaim binds claim verification to this client.
func (client *Client) VerifyDomainClaim() VerifyDomainClaimOperation {
	return VerifyDomainClaimOperation{client}
}

// Definition returns the generated claim creation contract.
func (CreateDomainClaimOperation) Definition() sdkgo.MutationDefinition {
	return CreateDomainClaimDefinition
}

// Definition returns the generated claim query contract.
func (GetDomainClaimOperation) Definition() sdkgo.QueryDefinition { return GetDomainClaimDefinition }

// Definition returns the generated claim verification contract.
func (VerifyDomainClaimOperation) Definition() sdkgo.MutationDefinition {
	return VerifyDomainClaimDefinition
}

// IdempotencyKey correlates bounded create attempts; the provider does not document a key here.
func (CreateDomainClaimOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDomainClaimInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless verification restarts.
func (VerifyDomainClaimOperation) IdempotencyKey(callID sdkgo.CallID, _ VerifyDomainClaimInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke starts or confirms a claim by domain name.
func (operation CreateDomainClaimOperation) Invoke(call sdkgo.Call, input CreateDomainClaimInput) sdkgo.MutationAttempt[DomainClaim] {
	if strings.TrimSpace(input.Name) == "" || strings.ContainsAny(input.Name, " /\t\n") || !validClaimRegion(input.Region) {
		return mutationFailure[DomainClaim]("createDomainClaim", CreateDomainClaimBranchDefect, sdkgo.FailureValidation, "domain name or region is invalid")
	}
	fields := map[string]any{"name": input.Name}
	if input.Region != "" {
		fields["region"] = input.Region
	}
	if input.CustomReturnPath != "" {
		fields["custom_return_path"] = input.CustomReturnPath
	}
	if input.OpenTracking != nil {
		fields["open_tracking"] = *input.OpenTracking
	}
	if input.ClickTracking != nil {
		fields["click_tracking"] = *input.ClickTracking
	}
	if input.TrackingSubdomain != "" {
		fields["tracking_subdomain"] = input.TrackingSubdomain
	}
	body, _ := json.Marshal(fields)
	request := resendLimitedCreate[DomainClaim]{client: operation.client, call: call, operation: "createDomainClaim", path: "/domains/claim", body: body, timeout: CreateDomainClaimDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateDomainClaimBranchCreated, rejected: CreateDomainClaimBranchProviderRejected, defect: CreateDomainClaimBranchDefect, decode: decodeDomainClaim, readBack: func(call sdkgo.Call) (DomainClaim, readBackState) { return operation.readBackClaim(call, input) }}
	return request.invoke()
}

// Invoke retrieves the latest claim of a placeholder domain.
func (operation GetDomainClaimOperation) Invoke(call sdkgo.Call, input GetDomainClaimInput) sdkgo.QueryAttempt[DomainClaim] {
	path, ok := domainClaimPath(input.DomainID)
	if !ok {
		return queryFailure[DomainClaim]("getDomainClaim", GetDomainClaimBranchDefect, sdkgo.FailureValidation, "placeholder domain ID is required")
	}
	return runQuery(operation.client, call, "getDomainClaim", path, GetDomainClaimBranchFound, GetDomainClaimBranchNotFound, GetDomainClaimBranchProviderRejected, GetDomainClaimBranchInvalidResponse, GetDomainClaimBranchDefect, decodeDomainClaim)
}

// Invoke starts asynchronous DNS verification for a claim.
func (operation VerifyDomainClaimOperation) Invoke(call sdkgo.Call, input VerifyDomainClaimInput) sdkgo.MutationAttempt[DomainClaim] {
	path, ok := domainClaimPath(input.DomainID)
	if !ok {
		return mutationFailure[DomainClaim]("verifyDomainClaim", VerifyDomainClaimBranchDefect, sdkgo.FailureValidation, "placeholder domain ID is required")
	}
	return runMutation(operation.client, call, "verifyDomainClaim", http.MethodPost, path+"/verify", nil, "", "", VerifyDomainClaimBranchVerificationStarted, VerifyDomainClaimBranchNotFound, VerifyDomainClaimBranchProviderRejected, VerifyDomainClaimBranchInvalidResponse, VerifyDomainClaimBranchDefect, decodeDomainClaim)
}

func (operation CreateDomainClaimOperation) readBackClaim(call sdkgo.Call, input CreateDomainClaimInput) (DomainClaim, readBackState) {
	body, state := operation.client.readBackResource(call, "/domains")
	if state != readBackMatched {
		return DomainClaim{}, state
	}
	var page DomainPage
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil {
		return DomainClaim{}, readBackRetry
	}
	for _, domain := range page.Data {
		if !strings.EqualFold(domain.Name, input.Name) {
			continue
		}
		path, ok := domainClaimPath(domain.ID)
		if !ok {
			return DomainClaim{}, readBackRetry
		}
		claimBody, claimState := operation.client.readBackResource(call, path)
		if claimState != readBackMatched {
			return DomainClaim{}, claimState
		}
		claim, err := decodeDomainClaim(claimBody)
		if err != nil {
			return DomainClaim{}, readBackRetry
		}
		if !strings.EqualFold(claim.Name, input.Name) || (input.Region != "" && claim.Region != input.Region) {
			return DomainClaim{}, readBackConflicted
		}
		return claim, readBackMatched
	}
	if page.HasMore {
		return DomainClaim{}, readBackRetry
	}
	return DomainClaim{}, readBackAbsent
}
func domainClaimPath(domainID string) (string, bool) {
	if strings.TrimSpace(domainID) == "" {
		return "", false
	}
	return "/domains/" + url.PathEscape(domainID) + "/claim", true
}
func validClaimRegion(region string) bool {
	switch region {
	case "", "us-east-1", "eu-west-1", "sa-east-1", "ap-northeast-1":
		return true
	}
	return false
}
func decodeDomainClaim(body []byte) (DomainClaim, error) {
	var value DomainClaim
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || value.Name == "" {
		return DomainClaim{}, fmt.Errorf("invalid domain claim")
	}
	return value, nil
}
