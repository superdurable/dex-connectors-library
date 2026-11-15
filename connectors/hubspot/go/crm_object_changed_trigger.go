// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MaximumTriggerProperties bounds CRMObjectChangedTriggerConfiguration.Properties; the Trigger always
// requests the record's creation and last-modified dates beside them.
const MaximumTriggerProperties = MaximumProperties - 2

// CRMObjectChangeKind is the derived kind of one crmObjectChanged delivery.
type CRMObjectChangeKind string

const (
	// CRMObjectChangeCreated is a record HubSpot created after the newest change an earlier poll of the
	// binding observed, so no earlier delivery or baseline included it.
	CRMObjectChangeCreated CRMObjectChangeKind = "created"
	// CRMObjectChangeUpdated is any other change to a record's properties, including a record that
	// appeared in search only after HubSpot's indexing delay.
	CRMObjectChangeUpdated CRMObjectChangeKind = "updated"
)

// CRMObjectChangedTriggerConfiguration is the crmObjectChanged binding configuration that the object type
// picker saves in Dex Web. Changing the object type of a binding that already polled makes its next poll
// record a new baseline for the new object type.
type CRMObjectChangedTriggerConfiguration struct {
	// ObjectType is the watched object type, contacts, companies, or deals, saved by the object type
	// picker. It is required.
	ObjectType ObjectType `json:"objectType"`
	// Properties lists internal property names, such as email or dealstage, to return on each delivered
	// record, at most MaximumTriggerProperties. Empty returns HubSpot's default search properties. The
	// Dex Web picker does not set it; an application sets it in code.
	Properties []string `json:"properties,omitempty"`
}

// Validate rejects a missing or unsupported object type, invalid property names, and a property list
// whose search request would exceed HubSpot's request body limit.
func (configuration CRMObjectChangedTriggerConfiguration) Validate() error {
	if configuration.ObjectType == "" {
		return errors.New("crmObjectChanged objectType is required")
	}
	if err := configuration.ObjectType.validate(); err != nil {
		return fmt.Errorf("crmObjectChanged %w", err)
	}
	if len(configuration.Properties) > MaximumTriggerProperties {
		return fmt.Errorf("crmObjectChanged allows at most %d properties", MaximumTriggerProperties)
	}
	if _, err := newCRMObjectChangeSearch(configuration, 0, strconv.Itoa(MaximumSearchResults-crmObjectChangePageSize)).searchRequestBody(); err != nil {
		return fmt.Errorf("crmObjectChanged search: %w", err)
	}
	return nil
}

// CRMObjectChangedEvent is one created or changed HubSpot CRM record read through CRM search. Several
// edits made between two polls arrive as one change carrying the record's latest values. Deleted,
// archived, and merged-away records are not reported, because CRM search does not return them.
type CRMObjectChangedEvent struct {
	// ObjectType is the watched object type.
	ObjectType ObjectType `json:"objectType"`
	// ChangeKind is the derived change kind; see CRMObjectChangeKind.
	ChangeKind CRMObjectChangeKind `json:"changeKind"`
	// ModifiedAt is the record's last-modified date, lastmodifieddate for contacts and
	// hs_lastmodifieddate otherwise, in UTC with millisecond precision.
	ModifiedAt time.Time `json:"modifiedAt"`
	// Object is the record as CRM search returned it, with the configured properties.
	Object CRMObject `json:"object"`
}

// lastModifiedProperty is the search property HubSpot updates on every change of an object type.
func (objectType ObjectType) lastModifiedProperty() string {
	if objectType == ObjectTypeContacts {
		return "lastmodifieddate"
	}
	return "hs_lastmodifieddate"
}

// newCRMObjectChangedTriggerEvent converts one changed record. Its ID is a pure function of the record
// version, so a refetch after a crash yields the same ID.
func newCRMObjectChangedTriggerEvent(object CRMObject, modifiedMilliseconds int64, kind CRMObjectChangeKind) sdkgo.TriggerEvent[CRMObjectChangedEvent] {
	modifiedAt := time.UnixMilli(modifiedMilliseconds).UTC()
	return sdkgo.TriggerEvent[CRMObjectChangedEvent]{
		ID:         crmObjectChangeID(object.ObjectType, object.ID, modifiedMilliseconds),
		OccurredAt: modifiedAt,
		Payload:    CRMObjectChangedEvent{ObjectType: object.ObjectType, ChangeKind: kind, ModifiedAt: modifiedAt, Object: object},
	}
}

func crmObjectChangeID(objectType ObjectType, objectID string, modifiedMilliseconds int64) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{string(objectType), objectID, strconv.FormatInt(modifiedMilliseconds, 10)}, "\x00")))
	return "hubspot-" + hex.EncodeToString(digest[:16])
}

// recordModifiedMilliseconds reads the last-modified property, falling back to the record's updatedAt.
func recordModifiedMilliseconds(object CRMObject) (int64, error) {
	if value, ok := object.Properties[object.ObjectType.lastModifiedProperty()]; ok {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return 0, errors.New("HubSpot returned an invalid last-modified date")
		}
		return parsed.UnixMilli(), nil
	}
	if object.UpdatedAt.IsZero() {
		return 0, errors.New("HubSpot returned a record without a last-modified date")
	}
	return object.UpdatedAt.UnixMilli(), nil
}

func crmObjectChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("hubspot crmObjectChanged configuration: %w", err)
}
