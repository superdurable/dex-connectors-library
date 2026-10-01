// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ColumnType names a monday.com column type, as monday.com's API spells it.
type ColumnType string

const (
	// ColumnTypeText is a single text column; its value is plain text.
	ColumnTypeText ColumnType = "text"
	// ColumnTypeLongText is a long text column of at most 2,000 characters.
	ColumnTypeLongText ColumnType = "long_text"
	// ColumnTypeNumbers is a numbers column.
	ColumnTypeNumbers ColumnType = "numbers"
	// ColumnTypeStatus is a status column, written by label text or label index.
	ColumnTypeStatus ColumnType = "status"
	// ColumnTypeDate is a date column with an optional UTC time.
	ColumnTypeDate ColumnType = "date"
	// ColumnTypePeople is a people column, written by monday.com user and team IDs.
	ColumnTypePeople ColumnType = "people"
	// ColumnTypeCheckbox is a checkbox column.
	ColumnTypeCheckbox ColumnType = "checkbox"
	// ColumnTypeEmail is an email column with display text.
	ColumnTypeEmail ColumnType = "email"
	// ColumnTypeLink is a link column with display text.
	ColumnTypeLink ColumnType = "link"
	// ColumnTypePhone is a phone column with an ISO 3166-1 alpha-2 country code.
	ColumnTypePhone ColumnType = "phone"
	// ColumnTypeDropdown is a dropdown column, written by label texts or label IDs.
	ColumnTypeDropdown ColumnType = "dropdown"
	// ColumnTypeTimeline is a timeline column, a date range.
	ColumnTypeTimeline ColumnType = "timeline"
)

const (
	// MaxColumnTextCharacters bounds one text or long_text value, monday.com's long text limit.
	MaxColumnTextCharacters = 2000
	// MaxColumnListEntries bounds the people, team, or dropdown entries of one value.
	MaxColumnListEntries     = 50
	maxColumnLabelCharacters = 255
	maxColumnURLCharacters   = 2000
)

var (
	calendarDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	clockTimePattern    = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}$`)
	phoneNumberPattern  = regexp.MustCompile(`^\+?[0-9]{4,20}$`)
	countryCodePattern  = regexp.MustCompile(`^[A-Z]{2}$`)
)

// ColumnValue is one typed value written to a monday.com column.
//
// Type selects which fields apply: Text for text and long_text; Number for
// numbers; StatusLabel or StatusIndex for status; Date and the optional Time for
// date; PersonIDs and TeamIDs for people; Checked for checkbox; Email with the
// optional Text for email; URL with the optional Text for link; Phone and
// CountryCode for phone; DropdownLabels or DropdownLabelIDs for dropdown; and
// Date and EndDate for timeline. Setting a field that does not belong to Type
// selects defect before any request is sent. Prefer the constructors such as
// TextValue and StatusLabelValue, and ClearedValue to empty a column on purpose.
//
// Writes replace the column's whole value: a people or dropdown value lists
// every entry the column keeps, so repeating the same write leaves the item
// unchanged.
type ColumnValue struct {
	// Type is the column's monday.com type.
	Type ColumnType `json:"type"`
	// IsCleared empties the column; no other field may be set.
	IsCleared bool `json:"cleared,omitempty"`
	// Text is the text of a text or long_text value, or the display text of an email or link.
	Text string `json:"text,omitempty"`
	// Number is a finite numbers value.
	Number *float64 `json:"number,omitempty"`
	// StatusLabel selects a status label by its text, such as Done.
	StatusLabel string `json:"statusLabel,omitempty"`
	// StatusIndex selects a status label by its index; monday.com recommends it for numeric labels.
	StatusIndex *int `json:"statusIndex,omitempty"`
	// Date is a YYYY-MM-DD date, or the start of a timeline.
	Date string `json:"date,omitempty"`
	// Time is an optional HH:MM:SS UTC time for a date value.
	Time string `json:"time,omitempty"`
	// EndDate is the YYYY-MM-DD end of a timeline, on or after Date.
	EndDate string `json:"endDate,omitempty"`
	// PersonIDs lists monday.com user IDs of a people value.
	PersonIDs []int64 `json:"personIds,omitempty"`
	// TeamIDs lists monday.com team IDs of a people value.
	TeamIDs []int64 `json:"teamIds,omitempty"`
	// Checked is a checkbox value; false clears the checkbox.
	Checked *bool `json:"checked,omitempty"`
	// Email is a bare email address such as jane@example.com.
	Email string `json:"email,omitempty"`
	// URL is an absolute http or https link.
	URL string `json:"url,omitempty"`
	// Phone is a phone number of 4 to 20 digits with an optional leading +.
	Phone string `json:"phone,omitempty"`
	// CountryCode is the phone number's ISO 3166-1 alpha-2 country code, such as US.
	CountryCode string `json:"countryCode,omitempty"`
	// DropdownLabels selects dropdown labels by their text.
	DropdownLabels []string `json:"dropdownLabels,omitempty"`
	// DropdownLabelIDs selects dropdown labels by their numeric IDs.
	DropdownLabelIDs []int `json:"dropdownLabelIds,omitempty"`
}

// TextValue returns a text column value.
func TextValue(text string) ColumnValue { return ColumnValue{Type: ColumnTypeText, Text: text} }

// LongTextValue returns a long_text column value.
func LongTextValue(text string) ColumnValue {
	return ColumnValue{Type: ColumnTypeLongText, Text: text}
}

// NumberValue returns a numbers column value.
func NumberValue(number float64) ColumnValue {
	return ColumnValue{Type: ColumnTypeNumbers, Number: &number}
}

// StatusLabelValue returns a status value selected by label text, such as Done.
func StatusLabelValue(label string) ColumnValue {
	return ColumnValue{Type: ColumnTypeStatus, StatusLabel: label}
}

// StatusIndexValue returns a status value selected by label index.
func StatusIndexValue(index int) ColumnValue {
	return ColumnValue{Type: ColumnTypeStatus, StatusIndex: &index}
}

// DateValue returns a date column value such as 2026-02-18.
func DateValue(date string) ColumnValue { return ColumnValue{Type: ColumnTypeDate, Date: date} }

// DateTimeValue returns a date column value with a UTC time such as 09:00:00.
func DateTimeValue(date string, utcTime string) ColumnValue {
	return ColumnValue{Type: ColumnTypeDate, Date: date, Time: utcTime}
}

// PeopleValue returns a people value that keeps exactly these user IDs.
func PeopleValue(personIDs ...int64) ColumnValue {
	return ColumnValue{Type: ColumnTypePeople, PersonIDs: personIDs}
}

// CheckboxValue returns a checkbox value.
func CheckboxValue(isChecked bool) ColumnValue {
	return ColumnValue{Type: ColumnTypeCheckbox, Checked: &isChecked}
}

// EmailValue returns an email value whose display text is the address.
func EmailValue(address string) ColumnValue {
	return ColumnValue{Type: ColumnTypeEmail, Email: address}
}

// LinkValue returns a link value with display text; blank text shows the URL.
func LinkValue(address string, text string) ColumnValue {
	return ColumnValue{Type: ColumnTypeLink, URL: address, Text: text}
}

// PhoneValue returns a phone value with its ISO 3166-1 alpha-2 country code.
func PhoneValue(phone string, countryCode string) ColumnValue {
	return ColumnValue{Type: ColumnTypePhone, Phone: phone, CountryCode: countryCode}
}

// DropdownLabelsValue returns a dropdown value that keeps exactly these label texts.
func DropdownLabelsValue(labels ...string) ColumnValue {
	return ColumnValue{Type: ColumnTypeDropdown, DropdownLabels: labels}
}

// TimelineValue returns a timeline value from start to end, both YYYY-MM-DD.
func TimelineValue(start string, end string) ColumnValue {
	return ColumnValue{Type: ColumnTypeTimeline, Date: start, EndDate: end}
}

// ClearedValue returns a value that empties a column of the given type.
func ClearedValue(columnType ColumnType) ColumnValue {
	return ColumnValue{Type: columnType, IsCleared: true}
}

// encodeColumnValues validates values and returns monday.com's column_values JSON string.
func encodeColumnValues(values map[string]ColumnValue, fieldName string) (string, error) {
	if len(values) > MaxRequestedColumns {
		return "", fmt.Errorf("%s holds at most %d columns", fieldName, MaxRequestedColumns)
	}
	columnIDs := make([]string, 0, len(values))
	for columnID := range values {
		columnIDs = append(columnIDs, columnID)
	}
	sort.Strings(columnIDs)
	encoded := make(map[string]any, len(values))
	for _, columnID := range columnIDs {
		switch {
		case columnID == "name":
			return "", fmt.Errorf("%s cannot set the name column; use the item name instead", fieldName)
		case !columnIDPattern.MatchString(columnID):
			return "", fmt.Errorf("%s keys must be monday.com column IDs such as status or date4", fieldName)
		}
		value, err := encodeColumnValue(values[columnID])
		if err != nil {
			return "", fmt.Errorf("%s %q: %w", fieldName, columnID, err)
		}
		encoded[columnID] = value
	}
	contents, err := json.Marshal(encoded)
	if err != nil {
		return "", fmt.Errorf("%s could not be encoded", fieldName)
	}
	return string(contents), nil
}

// encodeColumnValue returns the JSON value monday.com documents for one column type; nil clears the column.
func encodeColumnValue(value ColumnValue) (any, error) {
	if _, isSupported := columnTypeFields[value.Type]; !isSupported {
		return nil, errors.New("type must be a supported monday.com column type such as text, status, or date")
	}
	if err := rejectFieldsOutsideType(value); err != nil {
		return nil, err
	}
	if value.IsCleared {
		return nil, nil
	}
	switch value.Type {
	case ColumnTypeText:
		text, err := validateColumnText(value.Text)
		return text, err
	case ColumnTypeLongText:
		text, err := validateColumnText(value.Text)
		return map[string]any{"text": text}, err
	case ColumnTypeNumbers:
		if value.Number == nil || math.IsNaN(*value.Number) || math.IsInf(*value.Number, 0) {
			return nil, errors.New("a numbers value needs a finite number")
		}
		return strconv.FormatFloat(*value.Number, 'f', -1, 64), nil
	case ColumnTypeStatus:
		return encodeStatusValue(value)
	case ColumnTypeDate:
		return encodeDateValue(value)
	case ColumnTypePeople:
		return encodePeopleValue(value)
	case ColumnTypeCheckbox:
		if value.Checked == nil {
			return nil, errors.New("a checkbox value needs checked")
		}
		if !*value.Checked {
			return nil, nil
		}
		return map[string]any{"checked": "true"}, nil
	case ColumnTypeEmail:
		return encodeEmailValue(value)
	case ColumnTypeLink:
		return encodeLinkValue(value)
	case ColumnTypePhone:
		if !phoneNumberPattern.MatchString(value.Phone) || !countryCodePattern.MatchString(value.CountryCode) {
			return nil, errors.New("a phone value needs 4 to 20 digits with an optional + and a country code such as US")
		}
		return map[string]any{"phone": value.Phone, "countryShortName": value.CountryCode}, nil
	case ColumnTypeDropdown:
		return encodeDropdownValue(value)
	case ColumnTypeTimeline:
		if err := validateCalendarDate(value.Date); err != nil {
			return nil, fmt.Errorf("timeline start: %w", err)
		}
		if err := validateCalendarDate(value.EndDate); err != nil {
			return nil, fmt.Errorf("timeline end: %w", err)
		}
		if value.EndDate < value.Date {
			return nil, errors.New("a timeline ends on or after it starts")
		}
		return map[string]any{"from": value.Date, "to": value.EndDate}, nil
	default:
		return nil, errors.New("type must be a supported monday.com column type such as text, status, or date")
	}
}

// columnTypeFields lists the ColumnValue fields, by JSON name, that each supported column type reads.
var columnTypeFields = map[ColumnType][]string{
	ColumnTypeText: {"text"}, ColumnTypeLongText: {"text"}, ColumnTypeNumbers: {"number"},
	ColumnTypeStatus: {"statusLabel", "statusIndex"}, ColumnTypeDate: {"date", "time"},
	ColumnTypePeople: {"personIds", "teamIds"}, ColumnTypeCheckbox: {"checked"}, ColumnTypeEmail: {"email", "text"},
	ColumnTypeLink: {"url", "text"}, ColumnTypePhone: {"phone", "countryCode"},
	ColumnTypeDropdown: {"dropdownLabels", "dropdownLabelIds"}, ColumnTypeTimeline: {"date", "endDate"},
}

// rejectFieldsOutsideType keeps a typed value from silently dropping a field that another type uses.
func rejectFieldsOutsideType(value ColumnValue) error {
	allowed := columnTypeFields[value.Type]
	present := map[string]bool{
		"text": value.Text != "", "number": value.Number != nil, "statusLabel": value.StatusLabel != "",
		"statusIndex": value.StatusIndex != nil, "date": value.Date != "", "time": value.Time != "", "endDate": value.EndDate != "",
		"personIds": len(value.PersonIDs) != 0, "teamIds": len(value.TeamIDs) != 0, "checked": value.Checked != nil,
		"email": value.Email != "", "url": value.URL != "", "phone": value.Phone != "", "countryCode": value.CountryCode != "",
		"dropdownLabels": len(value.DropdownLabels) != 0, "dropdownLabelIds": len(value.DropdownLabelIDs) != 0,
	}
	isAllowed := map[string]bool{}
	for _, fieldName := range allowed {
		isAllowed[fieldName] = !value.IsCleared
	}
	fieldNames := make([]string, 0, len(present))
	for fieldName := range present {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		if present[fieldName] && !isAllowed[fieldName] {
			if value.IsCleared {
				return fmt.Errorf("a cleared value cannot set %s", fieldName)
			}
			return fmt.Errorf("%s does not apply to a %s column", fieldName, value.Type)
		}
	}
	return nil
}

func encodeStatusValue(value ColumnValue) (any, error) {
	switch {
	case (value.StatusLabel == "") == (value.StatusIndex == nil):
		return nil, errors.New("a status value sets exactly one of statusLabel and statusIndex")
	case value.StatusIndex != nil:
		if *value.StatusIndex < 0 {
			return nil, errors.New("statusIndex cannot be negative")
		}
		return map[string]any{"index": *value.StatusIndex}, nil
	}
	label, err := validateColumnLabel(value.StatusLabel, "statusLabel")
	return map[string]any{"label": label}, err
}

func encodeDateValue(value ColumnValue) (any, error) {
	if err := validateCalendarDate(value.Date); err != nil {
		return nil, err
	}
	encoded := map[string]any{"date": value.Date}
	if value.Time != "" {
		if _, err := time.Parse("15:04:05", value.Time); err != nil || !clockTimePattern.MatchString(value.Time) {
			return nil, errors.New("time must be an HH:MM:SS UTC time such as 09:00:00")
		}
		encoded["time"] = value.Time
	}
	return encoded, nil
}

func encodePeopleValue(value ColumnValue) (any, error) {
	if len(value.PersonIDs)+len(value.TeamIDs) == 0 {
		return nil, errors.New("a people value lists at least one person or team; use ClearedValue to empty it")
	}
	if len(value.PersonIDs)+len(value.TeamIDs) > MaxColumnListEntries {
		return nil, fmt.Errorf("a people value lists at most %d people and teams", MaxColumnListEntries)
	}
	entries := make([]map[string]any, 0, len(value.PersonIDs)+len(value.TeamIDs))
	for _, kindAndIDs := range []struct {
		kind string
		ids  []int64
	}{{"person", value.PersonIDs}, {"team", value.TeamIDs}} {
		for _, id := range kindAndIDs.ids {
			if id <= 0 {
				return nil, errors.New("people and team IDs must be positive monday.com IDs")
			}
			entries = append(entries, map[string]any{"id": id, "kind": kindAndIDs.kind})
		}
	}
	return map[string]any{"personsAndTeams": entries}, nil
}

func encodeEmailValue(value ColumnValue) (any, error) {
	address, err := mail.ParseAddress(value.Email)
	if err != nil || address.Name != "" || address.Address != value.Email {
		return nil, errors.New("email must be one bare address such as jane@example.com")
	}
	text := value.Email
	if value.Text != "" {
		if text, err = validateColumnLabel(value.Text, "text"); err != nil {
			return nil, err
		}
	}
	return map[string]any{"email": value.Email, "text": text}, nil
}

func encodeLinkValue(value ColumnValue) (any, error) {
	parsed, err := url.Parse(value.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || len(value.URL) > maxColumnURLCharacters {
		return nil, errors.New("url must be an absolute http or https URL")
	}
	text := value.URL
	if value.Text != "" {
		if text, err = validateColumnLabel(value.Text, "text"); err != nil {
			return nil, err
		}
	}
	return map[string]any{"url": value.URL, "text": text}, nil
}

func encodeDropdownValue(value ColumnValue) (any, error) {
	switch {
	case (len(value.DropdownLabels) == 0) == (len(value.DropdownLabelIDs) == 0):
		return nil, errors.New("a dropdown value sets exactly one of dropdownLabels and dropdownLabelIds; use ClearedValue to empty it")
	case len(value.DropdownLabels)+len(value.DropdownLabelIDs) > MaxColumnListEntries:
		return nil, fmt.Errorf("a dropdown value lists at most %d labels", MaxColumnListEntries)
	case len(value.DropdownLabelIDs) != 0:
		for _, id := range value.DropdownLabelIDs {
			if id < 0 {
				return nil, errors.New("dropdownLabelIds cannot be negative")
			}
		}
		return map[string]any{"ids": value.DropdownLabelIDs}, nil
	}
	labels := make([]string, 0, len(value.DropdownLabels))
	for _, label := range value.DropdownLabels {
		validated, err := validateColumnLabel(label, "dropdownLabels")
		if err != nil {
			return nil, err
		}
		labels = append(labels, validated)
	}
	return map[string]any{"labels": labels}, nil
}

func validateColumnText(text string) (string, error) {
	switch {
	case text == "":
		return "", errors.New("a text value needs text; use ClearedValue to empty the column")
	case !utf8.ValidString(text):
		return "", errors.New("text must be valid UTF-8")
	case utf8.RuneCountInString(text) > MaxColumnTextCharacters:
		return "", fmt.Errorf("text is at most %d characters", MaxColumnTextCharacters)
	}
	return text, nil
}

// validateColumnLabel checks a single-line label such as a status label, dropdown label, or display text.
func validateColumnLabel(label string, fieldName string) (string, error) {
	switch {
	case strings.TrimSpace(label) == "":
		return "", errors.New(fieldName + " cannot be blank")
	case !utf8.ValidString(label):
		return "", errors.New(fieldName + " must be valid UTF-8")
	case utf8.RuneCountInString(label) > maxColumnLabelCharacters:
		return "", fmt.Errorf("%s is at most %d characters", fieldName, maxColumnLabelCharacters)
	}
	for _, character := range label {
		if character < ' ' || character == 0x7f {
			return "", errors.New(fieldName + " must be one line without control characters")
		}
	}
	return label, nil
}

func validateCalendarDate(value string) error {
	if !calendarDatePattern.MatchString(value) {
		return errors.New("date must be a YYYY-MM-DD date such as 2026-02-18")
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return errors.New("date must be a real calendar date")
	}
	return nil
}
