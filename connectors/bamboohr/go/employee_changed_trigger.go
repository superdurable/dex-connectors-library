// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// EmployeeChangedTriggerConfiguration is the employeeChanged binding configuration that the
// changeTypePicker unit saves in Dex Web. Changing ChangeType of a binding that already polled makes its
// next poll record a new starting point, so changes made in between are not delivered.
type EmployeeChangedTriggerConfiguration struct {
	// ChangeType keeps one BambooHR change type: EmployeeChangeTypeInserted, EmployeeChangeTypeUpdated, or
	// EmployeeChangeTypeDeleted. Blank delivers every change. The connector sends it as BambooHR's type
	// parameter, so BambooHR filters the list.
	ChangeType EmployeeChangeType `json:"changeType,omitempty"`
}

// Validate rejects a change type other than inserted, updated, deleted, or blank.
func (configuration EmployeeChangedTriggerConfiguration) Validate() error {
	switch configuration.ChangeType {
	case "", EmployeeChangeTypeInserted, EmployeeChangeTypeUpdated, EmployeeChangeTypeDeleted:
		return nil
	default:
		return fmt.Errorf("BambooHR employeeChanged changeType must be inserted, updated, deleted, or blank, not %q", configuration.ChangeType)
	}
}

// EmployeeChangedEvent is one employee BambooHR lists as changed after the binding's cursor. BambooHR
// reports only each employee's latest change, so several edits between two polls arrive as one event
// whose Action is the latest one: an employee added and then edited before the next poll arrives as
// Updated. The event carries no field values; read the employee with getEmployee.
type EmployeeChangedEvent struct {
	// EmployeeID is BambooHR's internal employee ID, a digit string such as 140.
	EmployeeID string `json:"employeeId"`
	// Action is BambooHR's change action: Inserted, Updated, or Deleted. A termination is Updated.
	Action EmployeeChangeAction `json:"action"`
	// LastChanged is when BambooHR last changed the employee, in UTC with second precision.
	LastChanged time.Time `json:"lastChanged"`
}

// newEmployeeChangedTriggerEvent converts one listed change. OccurredAt is BambooHR's lastChanged.
func newEmployeeChangedTriggerEvent(change EmployeeChange) sdkgo.TriggerEvent[EmployeeChangedEvent] {
	return sdkgo.TriggerEvent[EmployeeChangedEvent]{
		ID:         employeeChangedEventID(change),
		OccurredAt: change.LastChanged,
		Payload:    EmployeeChangedEvent{EmployeeID: change.EmployeeID, Action: change.Action, LastChanged: change.LastChanged},
	}
}

// employeeChangedEventID is a pure function of the employee, action, and change instant, so a refetch
// after a crash yields the same ID and a later change to the same employee yields a new one.
func employeeChangedEventID(change EmployeeChange) string {
	return "employee-" + change.EmployeeID + "-" + strings.ToLower(string(change.Action)) + "-" +
		strconv.FormatInt(change.LastChanged.Unix(), 10)
}

func employeeChangedConfigurationError(err error) error {
	return fmt.Errorf("bamboohr employeeChanged configuration: %w", err)
}

var errEmployeeChangedCursorUndecodable = errors.New("BambooHR employeeChanged cursor is not decodable")
