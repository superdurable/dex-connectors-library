// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// recordChangedMaxBackoff caps the wait after consecutive failed recordChanged polls.
	recordChangedMaxBackoff = 5 * time.Minute
	// maximumRecordChangedPollPageSize is Airtable's largest list page.
	maximumRecordChangedPollPageSize = MaximumPageSize
)

var (
	tableIDPattern = regexp.MustCompile(`^tbl[0-9A-Za-z]{14}$`)
	fieldIDPattern = regexp.MustCompile(`^fld[0-9A-Za-z]{14}$`)
)

// RecordChangedTriggerConfiguration selects the table and Last modified time field a recordChanged binding
// polls. Dex Web saves it from the basePicker, tablePicker, and lastModifiedTimeFieldPicker units; the
// names they also save are shown in Dex Web only, so a renamed base, table, or field keeps working.
type RecordChangedTriggerConfiguration struct {
	// BaseID is the base ID, such as appXXXXXXXXXXXXXX. Required.
	BaseID string `json:"baseId"`
	// BaseName is the base name the picker showed; informational.
	BaseName string `json:"baseName,omitempty"`
	// TableID is the table ID, such as tblXXXXXXXXXXXXXX. Required.
	TableID string `json:"tableId"`
	// TableName is the table name the picker showed; informational.
	TableName string `json:"tableName,omitempty"`
	// LastModifiedTimeFieldID is the field ID, such as fldXXXXXXXXXXXXXX, of a Last modified time field
	// of the table whose value includes a time of day. Required. The field's own setting decides which
	// edits count as a change: all fields, or only the fields it watches.
	LastModifiedTimeFieldID string `json:"fieldId"`
	// LastModifiedTimeFieldName is the field name the picker showed; informational.
	LastModifiedTimeFieldName string `json:"fieldName,omitempty"`
}

// RecordChangedEvent is one record created or changed after the last processed change of the polled table.
type RecordChangedEvent struct {
	// BaseID is the polled base ID.
	BaseID string `json:"baseId"`
	// TableID is the polled table ID.
	TableID string `json:"tableId"`
	// LastModifiedTime is the record's Last modified time value when the poll read it, in UTC.
	LastModifiedTime time.Time `json:"lastModifiedTime"`
	// IsCreatedRecord reports that the record's createdTime is within one second of LastModifiedTime,
	// which is how a record that was created and not edited since reads.
	IsCreatedRecord bool `json:"isCreatedRecord"`
	// Record is the record as Airtable returned it, with fields keyed by field name.
	Record Record `json:"record"`
}

// Validate checks the base, table, and field IDs.
func (configuration RecordChangedTriggerConfiguration) Validate() error {
	if !baseIDPattern.MatchString(configuration.BaseID) {
		return errors.New("Airtable recordChanged baseId must be app followed by 14 letters and digits")
	}
	if !tableIDPattern.MatchString(configuration.TableID) {
		return errors.New("Airtable recordChanged tableId must be tbl followed by 14 letters and digits")
	}
	if !fieldIDPattern.MatchString(configuration.LastModifiedTimeFieldID) {
		return errors.New("Airtable recordChanged fieldId must be fld followed by 14 letters and digits")
	}
	return nil
}

// recordChangedTriggerSource serves the generated NewRecordChangedTrigger factory. Its cursor lives in
// process memory, so a restart seeds again; project applications use NewProjectRecordChangedTriggerRunner.
func (client *Client) recordChangedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration RecordChangedTriggerConfiguration,
) sdkgo.TriggerSource[RecordChangedEvent] {
	if err := configuration.Validate(); err != nil {
		return failedRecordChangedTriggerSource{err: err}
	}
	// The generated factory does not pass its binding name, so log records name the Trigger instead.
	binding := sdkgo.TriggerBindingRef{Connection: connection, Trigger: RecordChangedTriggerDefinition.Trigger, Name: RecordChangedTriggerDefinition.Trigger.TriggerName}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[RecordChangedEvent]{
		Definition: RecordChangedTriggerDefinition, Binding: binding, Cursor: pollingtrigger.NewMemoryCursorStore(),
		Fetcher: client.newRecordChangedFetcher(binding, configuration),
	}, client.recordChangedPollSchedule())
	if err != nil {
		return failedRecordChangedTriggerSource{err: err}
	}
	return source
}

// failedRecordChangedTriggerSource reports an invalid binding when its Trigger runs.
type failedRecordChangedTriggerSource struct{ err error }

// Run returns the configuration error without polling.
func (source failedRecordChangedTriggerSource) Run(context.Context, sdkgo.TriggerTarget[RecordChangedEvent]) error {
	return source.err
}

// recordChangedPollSchedule polls at the configured interval and backs off to five minutes.
func (client *Client) recordChangedPollSchedule() pollingtrigger.PollSchedule {
	return pollingtrigger.PollSchedule{Interval: client.pollInterval, MaxBackoff: max(recordChangedMaxBackoff, client.pollInterval)}
}

// recordChangedEventID is a pure function of the record and its modification time.
func recordChangedEventID(recordID string, lastModifiedTime time.Time) string {
	return recordID + "-" + strings.ReplaceAll(lastModifiedTime.UTC().Format("20060102T150405.000000000Z"), ".", "")
}
