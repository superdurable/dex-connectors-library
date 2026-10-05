// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// RecordTrigger is an automation Zoho CRM runs after a write through the API.
type RecordTrigger string

const (
	// RecordTriggerWorkflow runs the module's workflow rules.
	RecordTriggerWorkflow RecordTrigger = "workflow"
	// RecordTriggerApproval runs the module's approval processes.
	RecordTriggerApproval RecordTrigger = "approval"
	// RecordTriggerBlueprint runs the module's blueprints.
	RecordTriggerBlueprint RecordTrigger = "blueprint"
)

// writeRecordResult is one successful entry of an upsert or update response.
type writeRecordResult struct {
	action         string
	duplicateField string
	recordID       string
	createdAt      time.Time
	modifiedAt     time.Time
}

type writeResponseWire struct {
	Data []json.RawMessage `json:"data"`
}

type writeSuccessWire struct {
	Code           string          `json:"code"`
	Action         string          `json:"action"`
	DuplicateField json.RawMessage `json:"duplicate_field"`
	Details        struct {
		ID           string `json:"id"`
		CreatedTime  string `json:"Created_Time"`
		ModifiedTime string `json:"Modified_Time"`
	} `json:"details"`
}

// decodeWriteRecordResult reads a single-record write response: a success entry, or a record-level error summary.
func decodeWriteRecordResult(body []byte) (writeRecordResult, *zohoErrorSummary, error) {
	var response writeResponseWire
	if err := json.Unmarshal(body, &response); err != nil {
		return writeRecordResult{}, nil, errors.New("the response is not a JSON object with a data list")
	}
	if len(response.Data) != 1 {
		return writeRecordResult{}, nil, fmt.Errorf("the response holds %d results for one record", len(response.Data))
	}
	if summary, isError := describeRecordError(response.Data[0]); isError {
		return writeRecordResult{}, &summary, nil
	}
	var success writeSuccessWire
	if err := json.Unmarshal(response.Data[0], &success); err != nil || success.Code != "SUCCESS" {
		return writeRecordResult{}, nil, errors.New("the result is not a SUCCESS entry")
	}
	if !isRecordID(success.Details.ID) {
		return writeRecordResult{}, nil, errors.New("the result has no numeric record id")
	}
	result := writeRecordResult{action: success.Action, recordID: success.Details.ID}
	if !bytes.Equal(bytes.TrimSpace(success.DuplicateField), []byte("null")) && len(success.DuplicateField) != 0 {
		result.duplicateField = jsonStringMatching(success.DuplicateField, isFieldAPIName)
	}
	result.createdAt = parseOptionalZohoTime(success.Details.CreatedTime)
	result.modifiedAt = parseOptionalZohoTime(success.Details.ModifiedTime)
	return result, nil, nil
}

// renderTriggers returns nil to keep Zoho CRM's default, which runs every automation, or the explicit list.
func renderTriggers(triggers []RecordTrigger, shouldSkipAutomation bool) (*[]RecordTrigger, error) {
	if shouldSkipAutomation {
		if len(triggers) != 0 {
			return nil, errors.New("set triggers or shouldSkipAutomation, not both")
		}
		return &[]RecordTrigger{}, nil
	}
	if len(triggers) == 0 {
		return nil, nil
	}
	for index, trigger := range triggers {
		if trigger != RecordTriggerWorkflow && trigger != RecordTriggerApproval && trigger != RecordTriggerBlueprint {
			return nil, errors.New("each trigger must be workflow, approval, or blueprint")
		}
		if slices.Contains(triggers[:index], trigger) {
			return nil, errors.New("triggers must not repeat")
		}
	}
	listed := slices.Clone(triggers)
	return &listed, nil
}

func parseOptionalZohoTime(text string) time.Time {
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
