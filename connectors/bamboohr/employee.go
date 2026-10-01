// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxRequestedFields is BambooHR's documented limit on fields in one Get Employee request.
	MaxRequestedFields = 400
	// MaxWrittenFields bounds the fields one updateEmployee or addEmployee call sends.
	MaxWrittenFields = 50
	// MaxFieldValueBytes bounds one written field value, in UTF-8 bytes.
	MaxFieldValueBytes = 4096

	// civilDateLayout is BambooHR's date format, yyyy-mm-dd.
	civilDateLayout = "2006-01-02"
)

var (
	employeeIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	// fieldNamePattern matches standard field names and custom-field aliases such as customStartDate.
	fieldNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	// fieldIDPattern matches the numeric field IDs that List Fields returns, such as 1349.
	fieldIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)

	errEmployeeIDMismatch = errors.New("the returned employee ID is not the requested one")
)

// historyTableFields live in BambooHR history tables; a write can append a row, so a repeat could duplicate it.
var historyTableFields = []string{
	"jobTitle", "department", "division", "location", "reportsTo",
	"payRate", "payType", "payPer", "paidPer", "paySchedule", "overtimeRate", "exempt",
	"employmentHistoryStatus", "employmentStatus", "employeeStatusDate", "employmentType", "terminationDate",
}

// ignoredOnWriteFields are keys BambooHR's employee writes ignore or treat as identity.
var ignoredOnWriteFields = []string{"id", "employeeId", "photo", "photoUrl"}

// EmployeeRecord is one employee's requested fields as BambooHR returned them.
type EmployeeRecord struct {
	// EmployeeID is BambooHR's immutable internal employee ID, a string of digits such as 123.
	// It is not the editable Employee # (employeeNumber).
	EmployeeID string `json:"employeeId"`
	// Fields maps each returned field to its value. BambooHR returns most values as text; a null
	// value is empty, a boolean is true or false, and a structured value is its compact JSON.
	// A field requested by numeric ID may be returned under its name.
	Fields map[string]string `json:"fields"`
	// OmittedFields lists requested fields BambooHR did not return, sorted. BambooHR silently
	// omits a field that is unknown or that the connection's user cannot view.
	OmittedFields []string `json:"omittedFields,omitempty"`
}

// Value returns a field's value and whether BambooHR returned the field.
func (record EmployeeRecord) Value(field string) (string, bool) {
	value, isPresent := record.Fields[field]
	return value, isPresent
}

// decodeEmployeeObject decodes one employee JSON object into its ID and text field values.
func decodeEmployeeObject(contents json.RawMessage, idKey string) (string, map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var document map[string]json.RawMessage
	if err := decoder.Decode(&document); err != nil || document == nil {
		return "", nil, errors.New("the employee is not a JSON object")
	}
	employeeID, err := decodeEmployeeIDValue(document[idKey])
	if err != nil {
		return "", nil, err
	}
	fields := make(map[string]string, len(document))
	for name, raw := range document {
		if name == idKey {
			continue
		}
		value, err := fieldValueText(raw)
		if err != nil {
			return "", nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields[name] = value
	}
	return employeeID, fields, nil
}

// decodeEmployeeIDValue accepts BambooHR's string employee ID, or a number, as digits.
func decodeEmployeeIDValue(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("the employee has no ID")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return "", errors.New("the employee ID is not a string or number")
		}
		text = number.String()
	}
	if !employeeIDPattern.MatchString(text) {
		return "", errors.New("the employee ID is not a positive integer")
	}
	return text, nil
}

func fieldValueText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")):
		return "", nil
	case trimmed[0] == '"':
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return "", errors.New("invalid string")
		}
		return text, nil
	case trimmed[0] == '{' || trimmed[0] == '[':
		var compacted bytes.Buffer
		if err := json.Compact(&compacted, trimmed); err != nil {
			return "", errors.New("invalid structured value")
		}
		return compacted.String(), nil
	default:
		var scalar any
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if err := decoder.Decode(&scalar); err != nil {
			return "", errors.New("invalid value")
		}
		return fmt.Sprint(scalar), nil
	}
}

// omittedFields lists the requested fields absent from the returned values, sorted.
func omittedFields(requested []string, fields map[string]string) []string {
	var omitted []string
	for _, name := range requested {
		if _, isPresent := fields[name]; !isPresent {
			omitted = append(omitted, name)
		}
	}
	sort.Strings(omitted)
	return omitted
}

func validateEmployeeID(name string, employeeID string) error {
	if !employeeIDPattern.MatchString(employeeID) {
		return fmt.Errorf("%s must be BambooHR's internal employee ID, a positive integer such as 123; 0 and the editable Employee # are not accepted", name)
	}
	return nil
}

// validateRequestedFields accepts field names, custom-field aliases, and numeric field IDs.
func validateRequestedFields(name string, fields []string, minimum int, maximum int) error {
	if len(fields) < minimum || len(fields) > maximum {
		return fmt.Errorf("%s must name %d to %d BambooHR fields", name, minimum, maximum)
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if !fieldNamePattern.MatchString(field) && !fieldIDPattern.MatchString(field) {
			return fmt.Errorf("%s entry %q must be a BambooHR field name, custom-field alias, or numeric field ID", name, field)
		}
		if seen[field] {
			return fmt.Errorf("%s names %q twice", name, field)
		}
		seen[field] = true
	}
	return nil
}

// validateWrittenFields accepts names and aliases only, because a numeric field ID could name a history-table field.
func validateWrittenFields(name string, fields map[string]string, minimum int, refusedFields []string) error {
	if len(fields) < minimum || len(fields) > MaxWrittenFields {
		return fmt.Errorf("%s must hold %d to %d fields", name, minimum, MaxWrittenFields)
	}
	for field, value := range fields {
		if !fieldNamePattern.MatchString(field) {
			return fmt.Errorf("%s key %q must be a BambooHR field name or custom-field alias, not a numeric field ID", name, field)
		}
		if isFieldNamed(refusedFields, field) {
			return fmt.Errorf("%s cannot write %q through this operation", name, field)
		}
		if err := validateFieldValue(name+"."+field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldValue(name string, value string) error {
	if len(value) > MaxFieldValueBytes || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s must be valid UTF-8 text of at most %d bytes", name, MaxFieldValueBytes)
	}
	return nil
}

func isFieldNamed(names []string, field string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return strings.EqualFold(name, field) })
}

func isBareEmailAddress(value string) bool {
	if len(value) > 254 {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value
}

func validateCivilDate(name string, value string) error {
	parsed, err := time.Parse(civilDateLayout, value)
	if err != nil || parsed.Format(civilDateLayout) != value {
		return fmt.Errorf("%s must be a calendar date written YYYY-MM-DD", name)
	}
	return nil
}

func sortedFieldNames(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// compareEmployeeIDs orders BambooHR's digit-string IDs numerically.
func compareEmployeeIDs(left string, right string) int {
	if len(left) != len(right) {
		return len(left) - len(right)
	}
	return strings.Compare(left, right)
}
