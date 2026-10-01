// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

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

// PropertyType names a Notion page property type, as Notion's API spells it.
type PropertyType string

const (
	// PropertyTypeTitle is the data source's title property; its value is plain text.
	PropertyTypeTitle PropertyType = "title"
	// PropertyTypeRichText is a text property; its value is plain text.
	PropertyTypeRichText PropertyType = "rich_text"
	// PropertyTypeNumber is a number property.
	PropertyTypeNumber PropertyType = "number"
	// PropertyTypeCheckbox is a checkbox property.
	PropertyTypeCheckbox PropertyType = "checkbox"
	// PropertyTypeSelect is a single-option select property, written by option name.
	PropertyTypeSelect PropertyType = "select"
	// PropertyTypeMultiSelect is a multi-option select property, written by option names.
	PropertyTypeMultiSelect PropertyType = "multi_select"
	// PropertyTypeStatus is a status property, written by an existing option name.
	PropertyTypeStatus PropertyType = "status"
	// PropertyTypeDate is a date or date-time property, optionally a range.
	PropertyTypeDate PropertyType = "date"
	// PropertyTypeURL is a URL property.
	PropertyTypeURL PropertyType = "url"
	// PropertyTypeEmail is an email property.
	PropertyTypeEmail PropertyType = "email"
	// PropertyTypePhoneNumber is a phone number property, stored as free text.
	PropertyTypePhoneNumber PropertyType = "phone_number"
	// PropertyTypePeople is a people property, written by Notion user IDs.
	PropertyTypePeople PropertyType = "people"
	// PropertyTypeRelation is a relation property, written by related page IDs.
	PropertyTypeRelation PropertyType = "relation"
	// PropertyTypeFormula is a read-only formula result.
	PropertyTypeFormula PropertyType = "formula"
	// PropertyTypeRollup is a read-only rollup result.
	PropertyTypeRollup PropertyType = "rollup"
	// PropertyTypeCreatedTime is the read-only creation timestamp.
	PropertyTypeCreatedTime PropertyType = "created_time"
	// PropertyTypeLastEditedTime is the read-only last-edit timestamp.
	PropertyTypeLastEditedTime PropertyType = "last_edited_time"
	// PropertyTypeCreatedBy is the read-only creating user.
	PropertyTypeCreatedBy PropertyType = "created_by"
	// PropertyTypeLastEditedBy is the read-only last editing user.
	PropertyTypeLastEditedBy PropertyType = "last_edited_by"
	// PropertyTypeFiles is a files property; the connector reads file names only.
	PropertyTypeFiles PropertyType = "files"
	// PropertyTypeUniqueID is the read-only auto-increment ID, such as TASK-42.
	PropertyTypeUniqueID PropertyType = "unique_id"
	// PropertyTypeVerification is the read-only wiki verification state.
	PropertyTypeVerification PropertyType = "verification"
)

// TitlePropertyID is the ID of every data source's title property, whatever its
// name. Use it as a Properties key to write the title without knowing the
// column's name, such as Properties[notion.TitlePropertyID] = notion.TitleValue("Ada").
const TitlePropertyID = "title"

const (
	// maximumRichTextChunkUnits is Notion's limit for one rich text object's content, in UTF-16 code units.
	maximumRichTextChunkUnits = 2000
	// maximumArrayElements is Notion's limit for any array in a request body.
	maximumArrayElements = 100
	// MaximumPropertyTextCharacters bounds one title or rich_text value, split into 2,000-character objects.
	MaximumPropertyTextCharacters = 20000
	// MaximumProperties bounds the property values one create or update sends.
	MaximumProperties         = 100
	maximumPropertyKeyRunes   = 256
	maximumOptionNameRunes    = 100
	maximumURLCharacters      = 2000
	maximumEmailCharacters    = 200
	maximumPhoneNumberRunes   = 200
	maximumRollupArrayEntries = 25
)

var (
	datePattern     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeZonePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,3}$`)
)

// DateRange is a Notion date value: one date or date-time, or a range.
type DateRange struct {
	// Start is a YYYY-MM-DD date or an RFC 3339 date-time such as 2026-09-30T14:00:00Z.
	Start string `json:"start"`
	// End optionally ends a range, in the same format as Start; blank means a single date.
	End string `json:"end,omitempty"`
	// TimeZone optionally names an IANA time zone, such as America/Los_Angeles, for a
	// date-time without an offset; blank uses the offset in Start.
	TimeZone string `json:"timeZone,omitempty"`
}

// PropertyValue is one typed property value written to a Notion page.
//
// Type selects exactly one value field: Text for title and rich_text, Number,
// Checkbox, Option for select and status, Options for multi_select, Date, URL,
// Email, PhoneNumber, UserIDs for people, and PageIDs for relation. Setting a
// field that does not belong to Type selects defect. The zero value of the
// type's field clears the property, except that a checkbox needs an explicit
// value and a status needs an option name. Prefer the constructors such as
// TitleValue and SelectValue, and ClearedValue to clear a property on purpose.
//
// Writes replace the property's whole value: a multi_select, people, or
// relation value lists every entry the property keeps, so repeating the same
// write leaves the page unchanged.
type PropertyValue struct {
	// Type is the property's Notion type.
	Type PropertyType `json:"type"`
	// Text is the plain text of a title or rich_text value, at most 20,000 characters.
	Text string `json:"text,omitempty"`
	// Number is a finite number value.
	Number *float64 `json:"number,omitempty"`
	// Checkbox is a checkbox value.
	Checkbox *bool `json:"checkbox,omitempty"`
	// Option is a select or status option name. Notion adds a missing select
	// option to the data source; a status option must already exist.
	Option string `json:"option,omitempty"`
	// Options lists multi_select option names, at most 100, without commas.
	Options []string `json:"options,omitempty"`
	// Date is a date value.
	Date *DateRange `json:"date,omitempty"`
	// URL is an absolute URL value.
	URL string `json:"url,omitempty"`
	// Email is a plain email address.
	Email string `json:"email,omitempty"`
	// PhoneNumber is free-form phone text, such as +1 206 555 0100.
	PhoneNumber string `json:"phoneNumber,omitempty"`
	// UserIDs lists Notion user IDs for a people value, at most 100.
	UserIDs []string `json:"userIds,omitempty"`
	// PageIDs lists related page IDs for a relation value, at most 100.
	PageIDs []string `json:"pageIds,omitempty"`
}

// PageProperty is one property value read from a Notion page. The embedded
// PropertyValue holds the typed value; read-only types fill the closest typed
// field, such as Number for a numeric formula, Date for created_time, and
// UserIDs for created_by.
type PageProperty struct {
	// ID is Notion's stable property ID.
	ID string `json:"id"`
	PropertyValue
	// PlainText renders the value as one line of text for display and comparison.
	PlainText string `json:"plainText"`
	// IsTruncated reports that Notion returned only the first entries of a
	// relation, people, or rich text value; read the property in Notion for the rest.
	IsTruncated bool `json:"truncated,omitempty"`
}

// TitleValue returns a title value with plain text.
func TitleValue(text string) PropertyValue { return PropertyValue{Type: PropertyTypeTitle, Text: text} }

// RichTextValue returns a rich_text value with plain text.
func RichTextValue(text string) PropertyValue {
	return PropertyValue{Type: PropertyTypeRichText, Text: text}
}

// NumberValue returns a number value.
func NumberValue(number float64) PropertyValue {
	return PropertyValue{Type: PropertyTypeNumber, Number: &number}
}

// CheckboxValue returns a checkbox value.
func CheckboxValue(isChecked bool) PropertyValue {
	return PropertyValue{Type: PropertyTypeCheckbox, Checkbox: &isChecked}
}

// SelectValue returns a select value by option name.
func SelectValue(option string) PropertyValue {
	return PropertyValue{Type: PropertyTypeSelect, Option: option}
}

// MultiSelectValue returns a multi_select value that keeps exactly these option names.
func MultiSelectValue(options ...string) PropertyValue {
	return PropertyValue{Type: PropertyTypeMultiSelect, Options: options}
}

// StatusValue returns a status value by existing option name.
func StatusValue(option string) PropertyValue {
	return PropertyValue{Type: PropertyTypeStatus, Option: option}
}

// DateValue returns a single date or date-time value, such as 2026-09-30.
func DateValue(start string) PropertyValue {
	return PropertyValue{Type: PropertyTypeDate, Date: &DateRange{Start: start}}
}

// DateRangeValue returns a date range value.
func DateRangeValue(start string, end string) PropertyValue {
	return PropertyValue{Type: PropertyTypeDate, Date: &DateRange{Start: start, End: end}}
}

// URLValue returns a URL value.
func URLValue(address string) PropertyValue {
	return PropertyValue{Type: PropertyTypeURL, URL: address}
}

// EmailValue returns an email value.
func EmailValue(address string) PropertyValue {
	return PropertyValue{Type: PropertyTypeEmail, Email: address}
}

// PhoneNumberValue returns a phone number value.
func PhoneNumberValue(phoneNumber string) PropertyValue {
	return PropertyValue{Type: PropertyTypePhoneNumber, PhoneNumber: phoneNumber}
}

// PeopleValue returns a people value that keeps exactly these user IDs.
func PeopleValue(userIDs ...string) PropertyValue {
	return PropertyValue{Type: PropertyTypePeople, UserIDs: userIDs}
}

// RelationValue returns a relation value that keeps exactly these page IDs.
func RelationValue(pageIDs ...string) PropertyValue {
	return PropertyValue{Type: PropertyTypeRelation, PageIDs: pageIDs}
}

// ClearedValue returns a value that empties a property of the given type.
// Checkbox and status properties cannot be cleared.
func ClearedValue(propertyType PropertyType) PropertyValue { return PropertyValue{Type: propertyType} }

// encodePropertyValues validates values and returns Notion's properties object.
func encodePropertyValues(values map[string]PropertyValue, field string) (map[string]any, error) {
	if len(values) > MaximumProperties {
		return nil, fmt.Errorf("%s holds at most %d properties", field, MaximumProperties)
	}
	encoded := make(map[string]any, len(values))
	for _, key := range sortedPropertyKeys(values) {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" || trimmedKey != key || utf8.RuneCountInString(key) > maximumPropertyKeyRunes || strings.ContainsAny(key, "\r\n") {
			return nil, fmt.Errorf("%s keys are property names or IDs of 1 to %d characters without surrounding spaces", field, maximumPropertyKeyRunes)
		}
		value, err := encodePropertyValue(values[key])
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", field, key, err)
		}
		encoded[key] = value
	}
	return encoded, nil
}

func sortedPropertyKeys(values map[string]PropertyValue) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// encodePropertyValue maps one value onto Notion's property value object.
func encodePropertyValue(value PropertyValue) (map[string]any, error) {
	if field := value.foreignField(); field != "" {
		return nil, fmt.Errorf("a %s value cannot set %s", value.Type, field)
	}
	switch value.Type {
	case PropertyTypeTitle, PropertyTypeRichText:
		richText, err := encodeRichText(value.Text, MaximumPropertyTextCharacters)
		if err != nil {
			return nil, err
		}
		return map[string]any{string(value.Type): richText}, nil
	case PropertyTypeNumber:
		if value.Number == nil {
			return map[string]any{"number": nil}, nil
		}
		if math.IsNaN(*value.Number) || math.IsInf(*value.Number, 0) {
			return nil, errors.New("a number must be finite")
		}
		return map[string]any{"number": *value.Number}, nil
	case PropertyTypeCheckbox:
		if value.Checkbox == nil {
			return nil, errors.New("a checkbox needs true or false; it cannot be cleared")
		}
		return map[string]any{"checkbox": *value.Checkbox}, nil
	case PropertyTypeSelect:
		if value.Option == "" {
			return map[string]any{"select": nil}, nil
		}
		name, err := validateOptionName(value.Option)
		if err != nil {
			return nil, err
		}
		return map[string]any{"select": map[string]string{"name": name}}, nil
	case PropertyTypeStatus:
		if value.Option == "" {
			return nil, errors.New("a status needs an existing option name; it cannot be cleared")
		}
		name, err := validateOptionName(value.Option)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": map[string]string{"name": name}}, nil
	case PropertyTypeMultiSelect:
		return encodeMultiSelect(value.Options)
	case PropertyTypeDate:
		if value.Date == nil {
			return map[string]any{"date": nil}, nil
		}
		date, err := encodeDate(*value.Date)
		if err != nil {
			return nil, err
		}
		return map[string]any{"date": date}, nil
	case PropertyTypeURL:
		return encodeNullableText("url", value.URL, validateURLValue)
	case PropertyTypeEmail:
		return encodeNullableText("email", value.Email, validateEmailValue)
	case PropertyTypePhoneNumber:
		return encodeNullableText("phone_number", value.PhoneNumber, validatePhoneNumberValue)
	case PropertyTypePeople:
		ids, err := parseIDList(value.UserIDs, "userIds")
		if err != nil {
			return nil, err
		}
		people := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			people = append(people, map[string]string{"object": "user", "id": id})
		}
		return map[string]any{"people": people}, nil
	case PropertyTypeRelation:
		ids, err := parseIDList(value.PageIDs, "pageIds")
		if err != nil {
			return nil, err
		}
		relation := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			relation = append(relation, map[string]string{"id": id})
		}
		return map[string]any{"relation": relation}, nil
	case PropertyTypeFormula, PropertyTypeRollup, PropertyTypeCreatedTime, PropertyTypeLastEditedTime,
		PropertyTypeCreatedBy, PropertyTypeLastEditedBy, PropertyTypeUniqueID, PropertyTypeVerification, PropertyTypeFiles:
		return nil, fmt.Errorf("%s properties are not writable through this connector", value.Type)
	case "":
		return nil, errors.New("type is required")
	default:
		return nil, fmt.Errorf("type %q is not a Notion property type this connector writes", value.Type)
	}
}

// foreignField names the first value field set that does not belong to the value's type.
func (value PropertyValue) foreignField() string {
	owned := map[PropertyType]string{
		PropertyTypeTitle: "text", PropertyTypeRichText: "text", PropertyTypeNumber: "number",
		PropertyTypeCheckbox: "checkbox", PropertyTypeSelect: "option", PropertyTypeStatus: "option",
		PropertyTypeMultiSelect: "options", PropertyTypeDate: "date", PropertyTypeURL: "url",
		PropertyTypeEmail: "email", PropertyTypePhoneNumber: "phoneNumber", PropertyTypePeople: "userIds",
		PropertyTypeRelation: "pageIds",
	}[value.Type]
	for _, field := range []struct {
		name  string
		isSet bool
	}{
		{"text", value.Text != ""}, {"number", value.Number != nil}, {"checkbox", value.Checkbox != nil},
		{"option", value.Option != ""}, {"options", len(value.Options) > 0}, {"date", value.Date != nil},
		{"url", value.URL != ""}, {"email", value.Email != ""}, {"phoneNumber", value.PhoneNumber != ""},
		{"userIds", len(value.UserIDs) > 0}, {"pageIds", len(value.PageIDs) > 0},
	} {
		if field.isSet && field.name != owned {
			return field.name
		}
	}
	return ""
}

// encodeRichText splits text into rich text objects of at most 2,000 UTF-16 code units each.
func encodeRichText(text string, maximumCharacters int) ([]map[string]any, error) {
	if !utf8.ValidString(text) {
		return nil, errors.New("text must be valid UTF-8")
	}
	if utf8.RuneCountInString(text) > maximumCharacters {
		return nil, fmt.Errorf("text is at most %d characters", maximumCharacters)
	}
	chunks := splitTextIntoChunks(text, maximumRichTextChunkUnits)
	if len(chunks) > maximumArrayElements {
		return nil, fmt.Errorf("text needs more than %d rich text objects", maximumArrayElements)
	}
	richText := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		richText = append(richText, map[string]any{"type": "text", "text": map[string]string{"content": chunk}})
	}
	return richText, nil
}

// splitTextIntoChunks cuts text at rune boundaries into pieces of at most maximumUnits UTF-16 code units.
func splitTextIntoChunks(text string, maximumUnits int) []string {
	var chunks []string
	start, units := 0, 0
	for index, character := range text {
		width := 1
		if character > 0xFFFF {
			width = 2
		}
		if units+width > maximumUnits {
			chunks = append(chunks, text[start:index])
			start, units = index, 0
		}
		units += width
	}
	if start < len(text) {
		chunks = append(chunks, text[start:])
	}
	return chunks
}

func encodeMultiSelect(options []string) (map[string]any, error) {
	if len(options) > maximumArrayElements {
		return nil, fmt.Errorf("a multi_select value holds at most %d options", maximumArrayElements)
	}
	seen := map[string]bool{}
	encoded := make([]map[string]string, 0, len(options))
	for _, option := range options {
		name, err := validateOptionName(option)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("option %q is repeated", name)
		}
		seen[name] = true
		encoded = append(encoded, map[string]string{"name": name})
	}
	return map[string]any{"multi_select": encoded}, nil
}

func validateOptionName(option string) (string, error) {
	name := strings.TrimSpace(option)
	switch {
	case name == "":
		return "", errors.New("an option name cannot be blank")
	case utf8.RuneCountInString(name) > maximumOptionNameRunes:
		return "", fmt.Errorf("an option name is at most %d characters", maximumOptionNameRunes)
	case strings.Contains(name, ","):
		return "", errors.New("Notion option names cannot contain commas")
	case strings.ContainsAny(name, "\r\n"):
		return "", errors.New("an option name is one line")
	}
	return name, nil
}

func encodeDate(date DateRange) (map[string]any, error) {
	start, err := validateDateText(date.Start, "date start")
	if err != nil {
		return nil, err
	}
	encoded := map[string]any{"start": start}
	if strings.TrimSpace(date.End) != "" {
		end, err := validateDateText(date.End, "date end")
		if err != nil {
			return nil, err
		}
		encoded["end"] = end
	}
	if zone := strings.TrimSpace(date.TimeZone); zone != "" {
		if !timeZonePattern.MatchString(zone) || len(zone) > 64 {
			return nil, errors.New("date timeZone must be an IANA time zone name such as America/Los_Angeles")
		}
		encoded["time_zone"] = zone
	}
	return encoded, nil
}

func validateDateText(value string, field string) (string, error) {
	text := strings.TrimSpace(value)
	if datePattern.MatchString(text) {
		if _, err := time.Parse(time.DateOnly, text); err == nil {
			return text, nil
		}
	}
	if _, err := time.Parse(time.RFC3339, text); err == nil {
		return text, nil
	}
	return "", fmt.Errorf("%s must be a YYYY-MM-DD date or an RFC 3339 date-time", field)
}

func encodeNullableText(key string, value string, validate func(string) (string, error)) (map[string]any, error) {
	if value == "" {
		return map[string]any{key: nil}, nil
	}
	text, err := validate(value)
	if err != nil {
		return nil, err
	}
	return map[string]any{key: text}, nil
}

func validateURLValue(value string) (string, error) {
	text := strings.TrimSpace(value)
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || len(text) > maximumURLCharacters {
		return "", fmt.Errorf("a url value is an absolute URL of at most %d characters", maximumURLCharacters)
	}
	return text, nil
}

func validateEmailValue(value string) (string, error) {
	text := strings.TrimSpace(value)
	address, err := mail.ParseAddress(text)
	if err != nil || address.Address != text || len(text) > maximumEmailCharacters {
		return "", errors.New("an email value is one plain email address")
	}
	return text, nil
}

func validatePhoneNumberValue(value string) (string, error) {
	text := strings.TrimSpace(value)
	if utf8.RuneCountInString(text) > maximumPhoneNumberRunes || strings.ContainsAny(text, "\r\n") {
		return "", fmt.Errorf("a phone number is one line of at most %d characters", maximumPhoneNumberRunes)
	}
	return text, nil
}

func parseIDList(values []string, field string) ([]string, error) {
	if len(values) > maximumArrayElements {
		return nil, fmt.Errorf("%s holds at most %d IDs", field, maximumArrayElements)
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id, err := parseFieldID(value, field)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// propertyValueResource is one property value in a Notion page object.
type propertyValueResource struct {
	ID           string                `json:"id"`
	Type         string                `json:"type"`
	Title        []richTextResource    `json:"title"`
	RichText     []richTextResource    `json:"rich_text"`
	Number       *float64              `json:"number"`
	Checkbox     *bool                 `json:"checkbox"`
	Select       *optionResource       `json:"select"`
	Status       *optionResource       `json:"status"`
	MultiSelect  []optionResource      `json:"multi_select"`
	Date         *dateResource         `json:"date"`
	URL          *string               `json:"url"`
	Email        *string               `json:"email"`
	PhoneNumber  *string               `json:"phone_number"`
	People       []userResource        `json:"people"`
	Relation     []relationResource    `json:"relation"`
	HasMore      bool                  `json:"has_more"`
	Formula      *formulaResource      `json:"formula"`
	Rollup       *rollupResource       `json:"rollup"`
	CreatedTime  string                `json:"created_time"`
	EditedTime   string                `json:"last_edited_time"`
	CreatedBy    *userResource         `json:"created_by"`
	EditedBy     *userResource         `json:"last_edited_by"`
	Files        []fileResource        `json:"files"`
	UniqueID     *uniqueIDResource     `json:"unique_id"`
	Verification *verificationResource `json:"verification"`
}

type richTextResource struct {
	PlainText string `json:"plain_text"`
}

type optionResource struct {
	Name string `json:"name"`
}

type dateResource struct {
	Start    string  `json:"start"`
	End      *string `json:"end"`
	TimeZone *string `json:"time_zone"`
}

type userResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type relationResource struct {
	ID string `json:"id"`
}

type formulaResource struct {
	Type    string        `json:"type"`
	String  *string       `json:"string"`
	Number  *float64      `json:"number"`
	Boolean *bool         `json:"boolean"`
	Date    *dateResource `json:"date"`
}

type rollupResource struct {
	Type   string                  `json:"type"`
	Number *float64                `json:"number"`
	Date   *dateResource           `json:"date"`
	Array  []propertyValueResource `json:"array"`
}

type fileResource struct {
	Name string `json:"name"`
}

type uniqueIDResource struct {
	Prefix *string `json:"prefix"`
	Number *int64  `json:"number"`
}

type verificationResource struct {
	State string `json:"state"`
}

// decodePageProperties converts Notion's property map, keeping unknown types with their type name only.
func decodePageProperties(resources map[string]propertyValueResource) map[string]PageProperty {
	properties := make(map[string]PageProperty, len(resources))
	for name, resource := range resources {
		properties[name] = decodePageProperty(resource, true)
	}
	return properties
}

func decodePageProperty(resource propertyValueResource, canNest bool) PageProperty {
	property := PageProperty{ID: resource.ID, PropertyValue: PropertyValue{Type: PropertyType(resource.Type)}}
	switch property.Type {
	case PropertyTypeTitle:
		property.Text = joinPlainText(resource.Title)
		property.PlainText = property.Text
	case PropertyTypeRichText:
		property.Text = joinPlainText(resource.RichText)
		property.PlainText = property.Text
	case PropertyTypeNumber:
		property.Number = resource.Number
		property.PlainText = formatOptionalNumber(resource.Number)
	case PropertyTypeCheckbox:
		property.Checkbox = resource.Checkbox
		if resource.Checkbox != nil {
			property.PlainText = strconv.FormatBool(*resource.Checkbox)
		}
	case PropertyTypeSelect:
		property.Option = optionName(resource.Select)
		property.PlainText = property.Option
	case PropertyTypeStatus:
		property.Option = optionName(resource.Status)
		property.PlainText = property.Option
	case PropertyTypeMultiSelect:
		for _, option := range resource.MultiSelect {
			property.Options = append(property.Options, option.Name)
		}
		property.PlainText = strings.Join(property.Options, ", ")
	case PropertyTypeDate:
		property.Date, property.PlainText = decodeDate(resource.Date)
	case PropertyTypeURL:
		property.URL = dereferenceText(resource.URL)
		property.PlainText = property.URL
	case PropertyTypeEmail:
		property.Email = dereferenceText(resource.Email)
		property.PlainText = property.Email
	case PropertyTypePhoneNumber:
		property.PhoneNumber = dereferenceText(resource.PhoneNumber)
		property.PlainText = property.PhoneNumber
	case PropertyTypePeople:
		var labels []string
		for _, user := range resource.People {
			property.UserIDs = append(property.UserIDs, user.ID)
			labels = append(labels, userLabel(user))
		}
		property.PlainText = strings.Join(labels, ", ")
	case PropertyTypeRelation:
		for _, relation := range resource.Relation {
			property.PageIDs = append(property.PageIDs, relation.ID)
		}
		property.PlainText = strings.Join(property.PageIDs, ", ")
		property.IsTruncated = resource.HasMore
	case PropertyTypeFormula:
		decodeFormula(&property, resource.Formula)
	case PropertyTypeRollup:
		decodeRollup(&property, resource.Rollup, canNest)
	case PropertyTypeCreatedTime:
		property.Date, property.PlainText = &DateRange{Start: resource.CreatedTime}, resource.CreatedTime
	case PropertyTypeLastEditedTime:
		property.Date, property.PlainText = &DateRange{Start: resource.EditedTime}, resource.EditedTime
	case PropertyTypeCreatedBy:
		decodeUser(&property, resource.CreatedBy)
	case PropertyTypeLastEditedBy:
		decodeUser(&property, resource.EditedBy)
	case PropertyTypeFiles:
		var names []string
		for _, file := range resource.Files {
			names = append(names, file.Name)
		}
		property.PlainText = strings.Join(names, ", ")
	case PropertyTypeUniqueID:
		decodeUniqueID(&property, resource.UniqueID)
	case PropertyTypeVerification:
		if resource.Verification != nil {
			property.PlainText = resource.Verification.State
		}
	}
	return property
}

func decodeFormula(property *PageProperty, formula *formulaResource) {
	if formula == nil {
		return
	}
	switch formula.Type {
	case "string":
		property.Text = dereferenceText(formula.String)
		property.PlainText = property.Text
	case "number":
		property.Number = formula.Number
		property.PlainText = formatOptionalNumber(formula.Number)
	case "boolean":
		property.Checkbox = formula.Boolean
		if formula.Boolean != nil {
			property.PlainText = strconv.FormatBool(*formula.Boolean)
		}
	case "date":
		property.Date, property.PlainText = decodeDate(formula.Date)
	}
}

func decodeRollup(property *PageProperty, rollup *rollupResource, canNest bool) {
	if rollup == nil {
		return
	}
	switch rollup.Type {
	case "number":
		property.Number = rollup.Number
		property.PlainText = formatOptionalNumber(rollup.Number)
	case "date":
		property.Date, property.PlainText = decodeDate(rollup.Date)
	case "array":
		if !canNest {
			return
		}
		var texts []string
		for index, entry := range rollup.Array {
			if index == maximumRollupArrayEntries {
				property.IsTruncated = true
				break
			}
			if text := decodePageProperty(entry, false).PlainText; text != "" {
				texts = append(texts, text)
			}
		}
		property.PlainText = strings.Join(texts, ", ")
	}
}

func decodeUser(property *PageProperty, user *userResource) {
	if user == nil {
		return
	}
	property.UserIDs = []string{user.ID}
	property.PlainText = userLabel(*user)
}

func decodeUniqueID(property *PageProperty, uniqueID *uniqueIDResource) {
	if uniqueID == nil || uniqueID.Number == nil {
		return
	}
	number := float64(*uniqueID.Number)
	property.Number = &number
	property.Text = strconv.FormatInt(*uniqueID.Number, 10)
	if prefix := dereferenceText(uniqueID.Prefix); prefix != "" {
		property.Text = prefix + "-" + property.Text
	}
	property.PlainText = property.Text
}

func decodeDate(date *dateResource) (*DateRange, string) {
	if date == nil || date.Start == "" {
		return nil, ""
	}
	decoded := &DateRange{Start: date.Start, End: dereferenceText(date.End), TimeZone: dereferenceText(date.TimeZone)}
	if decoded.End == "" {
		return decoded, decoded.Start
	}
	return decoded, decoded.Start + "/" + decoded.End
}

func joinPlainText(richText []richTextResource) string {
	var builder strings.Builder
	for _, segment := range richText {
		builder.WriteString(segment.PlainText)
	}
	return builder.String()
}

func optionName(option *optionResource) string {
	if option == nil {
		return ""
	}
	return option.Name
}

func userLabel(user userResource) string {
	if user.Name != "" {
		return user.Name
	}
	return user.ID
}

func formatOptionalNumber(number *float64) string {
	if number == nil {
		return ""
	}
	return strconv.FormatFloat(*number, 'f', -1, 64)
}

func dereferenceText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// titleOfProperties returns the plain text of the page's title property.
func titleOfProperties(properties map[string]PageProperty) string {
	for _, property := range properties {
		if property.Type == PropertyTypeTitle {
			return property.Text
		}
	}
	return ""
}

// decodeRawProperties decodes the raw properties object of a page.
func decodeRawProperties(raw json.RawMessage) (map[string]PageProperty, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]PageProperty{}, nil
	}
	var resources map[string]propertyValueResource
	if err := json.Unmarshal(raw, &resources); err != nil {
		return nil, err
	}
	return decodePageProperties(resources), nil
}
