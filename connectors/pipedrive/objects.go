// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ObjectType names one Pipedrive record type. Its value is the API v2 path segment.
type ObjectType string

const (
	// ObjectTypePersons selects Pipedrive persons, the contacts a company deals with.
	ObjectTypePersons ObjectType = "persons"
	// ObjectTypeOrganizations selects Pipedrive organizations, the companies persons belong to.
	ObjectTypeOrganizations ObjectType = "organizations"
	// ObjectTypeDeals selects Pipedrive deals, which sit in one pipeline stage.
	ObjectTypeDeals ObjectType = "deals"
)

// DealStatus is Pipedrive's own deal status value, passed through unmapped.
type DealStatus string

const (
	// DealStatusOpen is a deal that is neither won nor lost.
	DealStatusOpen DealStatus = "open"
	// DealStatusWon is a deal marked won.
	DealStatusWon DealStatus = "won"
	// DealStatusLost is a deal marked lost.
	DealStatusLost DealStatus = "lost"
	// DealStatusDeleted is a deal deleted within the last 30 days; only listObjects accepts it.
	DealStatusDeleted DealStatus = "deleted"
)

// Record and write bounds. The field and value limits bound Flow payload size.
const (
	// MaximumWrittenFields bounds the standard plus custom field values of one write.
	MaximumWrittenFields = 100
	// MaximumFieldValueBytes bounds one encoded JSON field value.
	MaximumFieldValueBytes = 65536
	// MaximumPageLimit is Pipedrive's largest search and list page.
	MaximumPageLimit = 500
	// DefaultPageLimit is the page size used when a search or list Limit is zero.
	DefaultPageLimit = 25
	// MaximumListedCustomFields is Pipedrive's limit on custom field keys requested by one list.
	MaximumListedCustomFields = 15
)

var (
	recordIDPattern       = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	standardFieldPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	customFieldKeyPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	cursorPattern         = regexp.MustCompile(`^[A-Za-z0-9_\-=+/.]{1,1000}$`)
	errResponseMalformed  = errors.New("Pipedrive returned a malformed response")
)

// CRMObject is one Pipedrive person, organization, or deal. The typed fields
// expose the association graph, owner, and stage as decimal ID strings; Fields
// and CustomFields keep every returned value exactly as Pipedrive sent it.
type CRMObject struct {
	// ObjectType is the record's object type.
	ObjectType ObjectType `json:"objectType"`
	// ID is Pipedrive's positive integer record ID as a decimal string.
	ID string `json:"id"`
	// Name is a person's or organization's name, or a deal's title.
	Name string `json:"name,omitempty"`
	// OwnerID is the owning Pipedrive user's ID, when set.
	OwnerID string `json:"ownerId,omitempty"`
	// OrganizationID is the linked organization of a person or deal, when set.
	OrganizationID string `json:"organizationId,omitempty"`
	// PersonID is the linked person of a deal, when set.
	PersonID string `json:"personId,omitempty"`
	// PipelineID is a deal's pipeline.
	PipelineID string `json:"pipelineId,omitempty"`
	// StageID is a deal's stage.
	StageID string `json:"stageId,omitempty"`
	// Status is a deal's Pipedrive status, such as open, won, or lost.
	Status DealStatus `json:"status,omitempty"`
	// Emails lists a person's email addresses, primary first when Pipedrive marks one.
	Emails []string `json:"emails,omitempty"`
	// AddTime is when Pipedrive created the record, when reported.
	AddTime time.Time `json:"addTime,omitzero"`
	// UpdateTime is when Pipedrive last changed the record, when reported.
	UpdateTime time.Time `json:"updateTime,omitzero"`
	// IsPartial reports a search result, which carries only Pipedrive's search
	// summary; hydrate it with getObject.
	IsPartial bool `json:"isPartial,omitempty"`
	// Fields maps every returned standard field name to its exact JSON value.
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
	// CustomFields maps each returned custom field's 40-character key to its
	// exact JSON value; a search result lists none.
	CustomFields map[string]json.RawMessage `json:"customFields,omitempty"`
}

// ObjectPage is one bounded page of search or list results.
type ObjectPage struct {
	// ObjectType is the searched or listed object type.
	ObjectType ObjectType `json:"objectType"`
	// Objects are the records on this page, possibly none.
	Objects []CRMObject `json:"objects"`
	// NextCursor is the opaque cursor for the next page, empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DecodeField decodes the named standard field into destination and reports
// whether Pipedrive returned it. A JSON null leaves destination unchanged.
func (object CRMObject) DecodeField(fieldName string, destination any) (bool, error) {
	return decodeRawField(object.Fields, fieldName, destination)
}

// DecodeCustomField decodes the custom field with the 40-character key into
// destination and reports whether Pipedrive returned it. A JSON null leaves
// destination unchanged.
func (object CRMObject) DecodeCustomField(fieldKey string, destination any) (bool, error) {
	return decodeRawField(object.CustomFields, fieldKey, destination)
}

// StringValue encodes text as a JSON string for Fields or CustomFields.
func StringValue(text string) json.RawMessage {
	encoded, err := json.Marshal(text)
	if err != nil {
		// Marshaling a Go string cannot fail; invalid UTF-8 is replaced, not rejected.
		panic(err)
	}
	return encoded
}

// IDValue encodes a decimal Pipedrive ID, such as a picked stage or owner, as a
// JSON number for Fields. A value that is not a positive decimal ID produces
// invalid JSON, which the operation rejects with its defect branch.
func IDValue(id string) json.RawMessage {
	return json.RawMessage(id)
}

func decodeRawField(values map[string]json.RawMessage, name string, destination any) (bool, error) {
	value, isPresent := values[name]
	if !isPresent {
		return false, nil
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return true, nil
	}
	if err := json.Unmarshal(value, destination); err != nil {
		return true, fmt.Errorf("Pipedrive field %s cannot be decoded into the destination", name)
	}
	return true, nil
}

func (objectType ObjectType) validate() error {
	switch objectType {
	case ObjectTypePersons, ObjectTypeOrganizations, ObjectTypeDeals:
		return nil
	default:
		return errors.New("object type must be persons, organizations, or deals")
	}
}

// nameField is the standard field that names a record: title for deals, name otherwise.
func (objectType ObjectType) nameField() string {
	if objectType == ObjectTypeDeals {
		return "title"
	}
	return "name"
}

func validateRecordID(fieldName string, value string) error {
	if !recordIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a positive decimal Pipedrive ID", fieldName)
	}
	return nil
}

func validatePageLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultPageLimit, nil
	}
	if limit < 1 || limit > MaximumPageLimit {
		return 0, fmt.Errorf("limit must be from 1 through %d", MaximumPageLimit)
	}
	return limit, nil
}

func validateCursor(cursor string) error {
	if cursor != "" && !cursorPattern.MatchString(cursor) {
		return errors.New("cursor must be the nextCursor of a previous page")
	}
	return nil
}

// formatRecordID renders a JSON integer ID as a decimal string; null and absent IDs are empty.
func formatRecordID(value json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return "", errResponseMalformed
	}
	if _, err := strconv.ParseUint(number.String(), 10, 63); err != nil || !recordIDPattern.MatchString(number.String()) {
		return "", errResponseMalformed
	}
	return number.String(), nil
}

// validateWrittenFields checks field names and that every value is one bounded JSON value.
func validateWrittenFields(fields map[string]json.RawMessage, customFields map[string]json.RawMessage) error {
	if len(fields)+len(customFields) > MaximumWrittenFields {
		return fmt.Errorf("at most %d field values can be written", MaximumWrittenFields)
	}
	for name, value := range fields {
		if !standardFieldPattern.MatchString(name) || name == "id" || name == "custom_fields" {
			return errors.New("a written field name is invalid; put custom fields in customFields")
		}
		if err := validateFieldValue(value); err != nil {
			return fmt.Errorf("field %s %w", name, err)
		}
	}
	for key, value := range customFields {
		if !customFieldKeyPattern.MatchString(key) {
			return errors.New("a custom field key must be Pipedrive's 40-character lowercase hexadecimal key")
		}
		if err := validateFieldValue(value); err != nil {
			return fmt.Errorf("custom field %s %w", key, err)
		}
	}
	return nil
}

func validateFieldValue(value json.RawMessage) error {
	if len(value) > MaximumFieldValueBytes {
		return fmt.Errorf("value exceeds %d bytes", MaximumFieldValueBytes)
	}
	if len(bytes.TrimSpace(value)) == 0 || !json.Valid(value) {
		return errors.New("value must be one JSON value")
	}
	return nil
}

// requireNameField requires the record's naming field as a non-blank JSON string.
func requireNameField(objectType ObjectType, fields map[string]json.RawMessage) error {
	fieldName := objectType.nameField()
	var name string
	if json.Unmarshal(fields[fieldName], &name) != nil || len(bytes.TrimSpace([]byte(name))) == 0 {
		return fmt.Errorf("field %s must be a non-blank JSON string", fieldName)
	}
	return nil
}

// encodeWriteBody joins standard fields and the custom_fields object into one request body.
func encodeWriteBody(fields map[string]json.RawMessage, customFields map[string]json.RawMessage) ([]byte, error) {
	body := make(map[string]any, len(fields)+1)
	for name, value := range fields {
		body[name] = value
	}
	if len(customFields) > 0 {
		body["custom_fields"] = customFields
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, errors.New("the request body could not be encoded")
	}
	return encoded, nil
}

// pipedriveEnvelope is the success wrapper of every API v2 response.
type pipedriveEnvelope struct {
	Success        *bool           `json:"success"`
	Data           json.RawMessage `json:"data"`
	AdditionalData *struct {
		NextCursor *string `json:"next_cursor"`
	} `json:"additional_data"`
}

type searchItemsData struct {
	Items []struct {
		Item json.RawMessage `json:"item"`
	} `json:"items"`
}

type personEmail struct {
	Value     string `json:"value"`
	IsPrimary bool   `json:"primary"`
}

type linkedRecord struct {
	ID json.RawMessage `json:"id"`
}

func decodeEnvelope(body []byte) (pipedriveEnvelope, error) {
	var envelope pipedriveEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Success == nil || !*envelope.Success {
		return pipedriveEnvelope{}, errResponseMalformed
	}
	trimmed := bytes.TrimSpace(envelope.Data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return pipedriveEnvelope{}, errResponseMalformed
	}
	return envelope, nil
}

func (envelope pipedriveEnvelope) nextCursor() (string, error) {
	if envelope.AdditionalData == nil || envelope.AdditionalData.NextCursor == nil || *envelope.AdditionalData.NextCursor == "" {
		return "", nil
	}
	if !cursorPattern.MatchString(*envelope.AdditionalData.NextCursor) {
		return "", errors.New("Pipedrive returned an invalid next cursor")
	}
	return *envelope.AdditionalData.NextCursor, nil
}

// decodeExchangedRecord decodes a successful record exchange and turns a malformed or mismatched record into exchangeInvalid.
func decodeExchangedRecord(operation string, objectType ObjectType, objectID string, result pipedriveExchange) (CRMObject, pipedriveExchange) {
	if result.outcome != exchangeSucceeded {
		return CRMObject{}, result
	}
	object, err := decodeRecordBody(objectType, result.response.body)
	if err != nil || object.ID != objectID {
		result.outcome = exchangeInvalid
		result.failure = providerFailure(operation, sdkgo.FailureProtocol, "Pipedrive returned an invalid record")
		return CRMObject{}, result
	}
	return object, result
}

// decodeRecordBody decodes the single record of a get, create, or update response.
func decodeRecordBody(objectType ObjectType, body []byte) (CRMObject, error) {
	envelope, err := decodeEnvelope(body)
	if err != nil {
		return CRMObject{}, err
	}
	return decodeRecord(objectType, envelope.Data)
}

// decodeListPage decodes one list page of full records and its cursor.
func decodeListPage(objectType ObjectType, body []byte) (ObjectPage, error) {
	envelope, err := decodeEnvelope(body)
	if err != nil {
		return ObjectPage{}, err
	}
	var records []json.RawMessage
	if err := json.Unmarshal(envelope.Data, &records); err != nil {
		return ObjectPage{}, errResponseMalformed
	}
	page := ObjectPage{ObjectType: objectType, Objects: make([]CRMObject, 0, len(records))}
	for _, record := range records {
		object, err := decodeRecord(objectType, record)
		if err != nil {
			return ObjectPage{}, err
		}
		page.Objects = append(page.Objects, object)
	}
	page.NextCursor, err = envelope.nextCursor()
	return page, err
}

// decodeSearchPage decodes one search page of partial records and its cursor.
func decodeSearchPage(objectType ObjectType, body []byte) (ObjectPage, error) {
	envelope, err := decodeEnvelope(body)
	if err != nil {
		return ObjectPage{}, err
	}
	var data searchItemsData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return ObjectPage{}, errResponseMalformed
	}
	page := ObjectPage{ObjectType: objectType, Objects: make([]CRMObject, 0, len(data.Items))}
	for _, item := range data.Items {
		object, err := decodeSearchItem(objectType, item.Item)
		if err != nil {
			return ObjectPage{}, err
		}
		page.Objects = append(page.Objects, object)
	}
	page.NextCursor, err = envelope.nextCursor()
	return page, err
}

// decodeRecord reads one API v2 record, keeping every field's JSON value and the custom_fields object.
func decodeRecord(objectType ObjectType, record json.RawMessage) (CRMObject, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(record, &fields); err != nil || fields == nil {
		return CRMObject{}, errResponseMalformed
	}
	object := CRMObject{ObjectType: objectType, Fields: fields}
	if rawCustomFields, hasCustomFields := fields["custom_fields"]; hasCustomFields {
		delete(fields, "custom_fields")
		if !bytes.Equal(bytes.TrimSpace(rawCustomFields), []byte("null")) && json.Unmarshal(rawCustomFields, &object.CustomFields) != nil {
			return CRMObject{}, errResponseMalformed
		}
	}
	var err error
	if object.ID, err = formatRecordID(fields["id"]); err != nil || object.ID == "" {
		return CRMObject{}, errors.New("Pipedrive returned a record without a valid ID")
	}
	object.Name = stringFieldValue(fields[objectType.nameField()])
	relatedIDs := map[string]*string{"owner_id": &object.OwnerID}
	if objectType != ObjectTypeOrganizations {
		relatedIDs["org_id"] = &object.OrganizationID
	}
	if objectType == ObjectTypeDeals {
		relatedIDs["person_id"], relatedIDs["pipeline_id"], relatedIDs["stage_id"] = &object.PersonID, &object.PipelineID, &object.StageID
		object.Status = DealStatus(stringFieldValue(fields["status"]))
	}
	for fieldName, destination := range relatedIDs {
		if *destination, err = formatRecordID(fields[fieldName]); err != nil {
			return CRMObject{}, fmt.Errorf("Pipedrive returned an invalid %s", fieldName)
		}
	}
	if objectType == ObjectTypePersons {
		object.Emails = personEmailAddresses(fields["emails"])
	}
	object.AddTime = timeFieldValue(fields["add_time"])
	object.UpdateTime = timeFieldValue(fields["update_time"])
	return object, nil
}

// decodeSearchItem reads one search summary, whose owner and links are nested objects.
func decodeSearchItem(objectType ObjectType, item json.RawMessage) (CRMObject, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(item, &fields); err != nil || fields == nil {
		return CRMObject{}, errResponseMalformed
	}
	object := CRMObject{ObjectType: objectType, Fields: fields, IsPartial: true}
	var err error
	if object.ID, err = formatRecordID(fields["id"]); err != nil || object.ID == "" {
		return CRMObject{}, errors.New("Pipedrive returned a search result without a valid ID")
	}
	object.Name = stringFieldValue(fields[objectType.nameField()])
	for fieldName, destination := range map[string]*string{
		"owner": &object.OwnerID, "organization": &object.OrganizationID, "person": &object.PersonID, "stage": &object.StageID,
	} {
		var linked linkedRecord
		if raw, isPresent := fields[fieldName]; isPresent && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if json.Unmarshal(raw, &linked) != nil {
				return CRMObject{}, fmt.Errorf("Pipedrive returned an invalid %s in a search result", fieldName)
			}
			if *destination, err = formatRecordID(linked.ID); err != nil {
				return CRMObject{}, fmt.Errorf("Pipedrive returned an invalid %s in a search result", fieldName)
			}
		}
	}
	if objectType == ObjectTypeDeals {
		object.Status = DealStatus(stringFieldValue(fields["status"]))
	}
	if objectType == ObjectTypePersons {
		var emails []string
		if json.Unmarshal(fields["emails"], &emails) == nil {
			object.Emails = emails
		}
	}
	return object, nil
}

func stringFieldValue(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) != nil {
		return ""
	}
	return text
}

// timeFieldValue parses an RFC 3339 API v2 timestamp; any other form stays only in Fields.
func timeFieldValue(value json.RawMessage) time.Time {
	parsed, err := time.Parse(time.RFC3339, stringFieldValue(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

// personEmailAddresses lists the primary address first, then the others in Pipedrive's order.
func personEmailAddresses(value json.RawMessage) []string {
	var emails []personEmail
	if json.Unmarshal(value, &emails) != nil {
		return nil
	}
	var primary, others []string
	for _, email := range emails {
		switch {
		case email.Value == "":
		case email.IsPrimary:
			primary = append(primary, email.Value)
		default:
			others = append(others, email.Value)
		}
	}
	return append(primary, others...)
}
