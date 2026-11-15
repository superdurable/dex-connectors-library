// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxTriggerFieldNames bounds the extra fields one recordModifiedPolled binding reads with each record.
const MaxTriggerFieldNames = 50

// triggerSystemFields are selected for every record; they order the poll and derive the change kind.
var triggerSystemFields = []string{"Id", "CreatedDate", "LastModifiedDate", "SystemModstamp"}

// RecordChangeKind is the derived kind of one recordModifiedPolled delivery.
type RecordChangeKind string

const (
	// RecordChangeCreated is a record whose LastModifiedDate still equals its CreatedDate.
	RecordChangeCreated RecordChangeKind = "created"
	// RecordChangeUpdated is any other change that moved SystemModstamp, including an edit made soon after
	// creation but before the next poll read it, and a system change such as a roll-up summary recalculation
	// that leaves LastModifiedDate unchanged.
	RecordChangeUpdated RecordChangeKind = "updated"
)

// RecordModifiedTriggerConfiguration is the recordModifiedPolled binding configuration that the object
// picker and field list save in Dex Web. Changing the object of a binding that already polled makes its
// next poll record a new baseline for the new object; changing the field list keeps the cursor.
type RecordModifiedTriggerConfiguration struct {
	// SObjectType is the watched object's API name, such as Opportunity or Invoice__c; it is required.
	SObjectType string `json:"sObjectType"`
	// FieldNames lists up to MaxTriggerFieldNames extra field API names, such as StageName, read with each
	// record. Id, CreatedDate, LastModifiedDate, and SystemModstamp are always read; blank reads only those.
	FieldNames []string `json:"fieldNames,omitempty"`
}

// Validate requires an object API name and rejects invalid or repeated field API names.
func (configuration RecordModifiedTriggerConfiguration) Validate() error {
	if !isAPIName(configuration.SObjectType) {
		return errors.New("recordModifiedPolled sObjectType must be an object API name such as Opportunity")
	}
	if len(configuration.FieldNames) > MaxTriggerFieldNames {
		return fmt.Errorf("recordModifiedPolled fieldNames must hold at most %d field API names", MaxTriggerFieldNames)
	}
	seen := map[string]bool{}
	for _, fieldName := range configuration.FieldNames {
		if !isFieldAPIName(fieldName) {
			return errors.New("recordModifiedPolled fieldNames must be field API names such as StageName, without relationship dots")
		}
		if seen[strings.ToLower(fieldName)] {
			return errors.New("recordModifiedPolled fieldNames must not repeat a field API name")
		}
		seen[strings.ToLower(fieldName)] = true
	}
	return nil
}

// selectedFields returns the system fields followed by each configured field that is not one of them.
func (configuration RecordModifiedTriggerConfiguration) selectedFields() []string {
	fields := append([]string(nil), triggerSystemFields...)
	for _, fieldName := range configuration.FieldNames {
		isSystemField := false
		for _, systemField := range triggerSystemFields {
			isSystemField = isSystemField || strings.EqualFold(fieldName, systemField)
		}
		if !isSystemField {
			fields = append(fields, fieldName)
		}
	}
	return fields
}

// RecordModifiedEvent is one created or modified record read by polling. A record changed several times
// between two polls arrives once, in its latest state; a deleted record is not delivered.
type RecordModifiedEvent struct {
	// SObjectType is the watched object's API name.
	SObjectType string `json:"sObjectType"`
	// RecordID is the record's 18-character ID.
	RecordID string `json:"recordId"`
	// ChangeKind is the derived change kind; see RecordChangeKind.
	ChangeKind RecordChangeKind `json:"changeKind"`
	// SystemModstamp is the record's SystemModstamp in UTC, the time of the change this event reports.
	SystemModstamp time.Time `json:"systemModstamp"`
	// Record holds Id, CreatedDate, LastModifiedDate, SystemModstamp, and every configured field as
	// Salesforce returned them, with the field, object, and sharing access of the authorized user.
	Record Record `json:"record"`
}

// newRecordModifiedTriggerEvent converts one polled record. Its ID is a pure function of the record and its
// SystemModstamp, so a refetch after a crash yields the same ID and a later change yields a new one.
func newRecordModifiedTriggerEvent(sObjectType string, record Record, modstamp time.Time, changeKind RecordChangeKind) (string, RecordModifiedEvent) {
	digest := sha256.Sum256([]byte(strings.Join([]string{sObjectType, record.ID, modstamp.UTC().Format(time.RFC3339)}, "\x00")))
	return "sfdc-" + hex.EncodeToString(digest[:16]), RecordModifiedEvent{
		SObjectType: sObjectType, RecordID: record.ID, ChangeKind: changeKind, SystemModstamp: modstamp.UTC(), Record: record,
	}
}

// recordChangeKind compares CreatedDate with LastModifiedDate; an unreadable pair counts as updated.
func recordChangeKind(record Record) RecordChangeKind {
	createdText, hasCreated := record.StringField("CreatedDate")
	modifiedText, hasModified := record.StringField("LastModifiedDate")
	if !hasCreated || !hasModified {
		return RecordChangeUpdated
	}
	created, createdErr := parseSalesforceDateTime(createdText)
	modified, modifiedErr := parseSalesforceDateTime(modifiedText)
	if createdErr == nil && modifiedErr == nil && created.Equal(modified) {
		return RecordChangeCreated
	}
	return RecordChangeUpdated
}

// salesforceDateTimeLayouts are the REST API's dateTime form, such as 2026-03-02T09:00:00.000+0000, and RFC 3339.
var salesforceDateTimeLayouts = []string{"2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05.000Z0700", time.RFC3339Nano}

func parseSalesforceDateTime(value string) (time.Time, error) {
	for _, layout := range salesforceDateTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("Salesforce dateTime value is not in the REST API form")
}

func recordModifiedTriggerConfigurationError(err error) error {
	return fmt.Errorf("salesforce recordModifiedPolled configuration: %w", err)
}
