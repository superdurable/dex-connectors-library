// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetCompanyInput selects the company for the connected Harvest account.
type GetCompanyInput struct{}

// UpdateCompanyInput changes the two mutable Harvest company settings.
type UpdateCompanyInput struct {
	// WantsTimestampTimers selects start-and-end tracking when true, or duration tracking when false.
	WantsTimestampTimers *bool `json:"wantsTimestampTimers,omitempty"`
	// WeeklyCapacitySeconds is the positive number of working seconds per week.
	WeeklyCapacitySeconds *int `json:"weeklyCapacitySeconds,omitempty"`
}

// CompanyRecord contains the account's company settings.
type CompanyRecord struct {
	// Name is the company name.
	Name string `json:"name"`
	// FullDomain is the Harvest account's domain.
	FullDomain string `json:"fullDomain"`
	// IsActive reports whether the company is active.
	IsActive bool `json:"isActive"`
	// WantsTimestampTimers identifies the account's timer style.
	WantsTimestampTimers bool `json:"wantsTimestampTimers"`
	// WeeklyCapacitySeconds is the company's weekly capacity in seconds.
	WeeklyCapacitySeconds int `json:"weeklyCapacitySeconds"`
}

// GetCompanyOperation reads the connected Harvest company's settings.
type GetCompanyOperation struct{ client *Client }

// GetCompany binds the company lookup Query to this client.
func (client *Client) GetCompany() GetCompanyOperation { return GetCompanyOperation{client} }

// Definition returns the generated company Query contract.
func (GetCompanyOperation) Definition() sdkgo.QueryDefinition { return GetCompanyDefinition }

// Invoke reads the company settings.
func (operation GetCompanyOperation) Invoke(call sdkgo.Call, _ GetCompanyInput) sdkgo.QueryAttempt[CompanyRecord] {
	return invokeGet(operation.client, call, "getCompany", "/company", GetCompanyBranchFound, GetCompanyBranchProviderRejected, decodeCompanyRecord)
}

// UpdateCompanyOperation sets absolute company settings.
type UpdateCompanyOperation struct{ client *Client }

// UpdateCompany binds the company update Mutation to this client.
func (client *Client) UpdateCompany() UpdateCompanyOperation { return UpdateCompanyOperation{client} }

// Definition returns the generated company Mutation contract.
func (UpdateCompanyOperation) Definition() sdkgo.MutationDefinition { return UpdateCompanyDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateCompanyOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies an idempotent settings PATCH.
func (operation UpdateCompanyOperation) Invoke(call sdkgo.Call, input UpdateCompanyInput) sdkgo.MutationAttempt[CompanyRecord] {
	if input.WantsTimestampTimers == nil && input.WeeklyCapacitySeconds == nil || input.WeeklyCapacitySeconds != nil && (*input.WeeklyCapacitySeconds <= 0 || *input.WeeklyCapacitySeconds > 604800) {
		return harvestMutationFailure[CompanyRecord]("updateCompany", UpdateCompanyBranchDefect, sdkgo.FailureValidation, "company settings are invalid")
	}
	payload, err := json.Marshal(struct {
		WantsTimestampTimers  *bool `json:"wants_timestamp_timers,omitempty"`
		WeeklyCapacitySeconds *int  `json:"weekly_capacity,omitempty"`
	}{input.WantsTimestampTimers, input.WeeklyCapacitySeconds})
	if err != nil {
		return harvestMutationFailure[CompanyRecord]("updateCompany", UpdateCompanyBranchDefect, sdkgo.FailureValidation, "company update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateCompany", http.MethodPatch, "/company", payload, UpdateCompanyBranchUpdated, UpdateCompanyBranchProviderRejected, UpdateCompanyBranchProviderRejected, decodeCompanyRecord)
}

func decodeCompanyRecord(body []byte) (CompanyRecord, error) {
	var wire struct {
		Name                  string `json:"name"`
		FullDomain            string `json:"full_domain"`
		IsActive              bool   `json:"is_active"`
		WantsTimestampTimers  bool   `json:"wants_timestamp_timers"`
		WeeklyCapacitySeconds int    `json:"weekly_capacity"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CompanyRecord{}, err
	}
	if wire.Name == "" || wire.FullDomain == "" || wire.WeeklyCapacitySeconds < 0 {
		return CompanyRecord{}, fmt.Errorf("missing company settings")
	}
	return CompanyRecord{Name: wire.Name, FullDomain: wire.FullDomain, IsActive: wire.IsActive, WantsTimestampTimers: wire.WantsTimestampTimers, WeeklyCapacitySeconds: wire.WeeklyCapacitySeconds}, nil
}
