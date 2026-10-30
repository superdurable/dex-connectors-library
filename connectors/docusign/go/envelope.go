// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// EnvelopeStatus is DocuSign's envelope status, such as sent or completed, unchanged and lowercase.
type EnvelopeStatus string

const (
	// EnvelopeStatusCreated is a draft envelope that has not been sent.
	EnvelopeStatusCreated EnvelopeStatus = "created"
	// EnvelopeStatusSent means DocuSign emailed the first routing order's recipients.
	EnvelopeStatusSent EnvelopeStatus = "sent"
	// EnvelopeStatusDelivered means every current recipient opened the envelope.
	EnvelopeStatusDelivered EnvelopeStatus = "delivered"
	// EnvelopeStatusSigned means the recipients signed but DocuSign has not completed the envelope yet.
	EnvelopeStatusSigned EnvelopeStatus = "signed"
	// EnvelopeStatusCompleted means every recipient finished; the signed documents are final.
	EnvelopeStatusCompleted EnvelopeStatus = "completed"
	// EnvelopeStatusDeclined means a recipient declined to sign.
	EnvelopeStatusDeclined EnvelopeStatus = "declined"
	// EnvelopeStatusVoided means the sender voided the envelope or it expired.
	EnvelopeStatusVoided EnvelopeStatus = "voided"
)

// idempotencyCustomFieldName is the reserved hidden field holding the Step's idempotency key.
const idempotencyCustomFieldName = "dexIdempotencyKey"

var envelopeStatusPattern = regexp.MustCompile(`^[a-z]{1,32}$`)

// IsTerminal reports whether DocuSign can no longer change the envelope's outcome: completed,
// declined, or voided.
func (status EnvelopeStatus) IsTerminal() bool {
	return status == EnvelopeStatusCompleted || status == EnvelopeStatusDeclined || status == EnvelopeStatusVoided
}

// EnvelopeCustomField is one envelope text custom field, hidden from recipients, such as a correlation
// ID that a Connect event carries back.
type EnvelopeCustomField struct {
	// Name is the field name, 1 to 50 characters.
	Name string `json:"name"`
	// Value is the field value, at most 100 characters.
	Value string `json:"value"`
}

// Envelope is one DocuSign envelope's status and timestamps. Each timestamp is nil until DocuSign
// records the matching transition.
type Envelope struct {
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
	// Status is DocuSign's current status.
	Status EnvelopeStatus `json:"status"`
	// EmailSubject is the subject of the envelope's email.
	EmailSubject string `json:"emailSubject,omitempty"`
	// CreatedAt is when the envelope was created.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// SentAt is when DocuSign sent the envelope.
	SentAt *time.Time `json:"sentAt,omitempty"`
	// DeliveredAt is when every current recipient had opened it.
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
	// CompletedAt is when the last recipient finished.
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// DeclinedAt is when a recipient declined.
	DeclinedAt *time.Time `json:"declinedAt,omitempty"`
	// VoidedAt is when the envelope was voided or expired.
	VoidedAt *time.Time `json:"voidedAt,omitempty"`
	// StatusChangedAt is when Status last changed.
	StatusChangedAt *time.Time `json:"statusChangedAt,omitempty"`
	// ExpiresAt is when an unfinished envelope expires and DocuSign voids it.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// VoidedReason is the void reason the sender gave or DocuSign recorded for an expiry.
	VoidedReason string `json:"voidedReason,omitempty"`
	// CustomFields holds the envelope's text custom fields, including the connector's dexIdempotencyKey.
	CustomFields []EnvelopeCustomField `json:"customFields,omitempty"`
}

// docusignEnvelopeResource is the part of an envelope resource the connector reads.
type docusignEnvelopeResource struct {
	EnvelopeID            string                  `json:"envelopeId"`
	Status                string                  `json:"status"`
	EmailSubject          string                  `json:"emailSubject"`
	CreatedDateTime       string                  `json:"createdDateTime"`
	SentDateTime          string                  `json:"sentDateTime"`
	DeliveredDateTime     string                  `json:"deliveredDateTime"`
	CompletedDateTime     string                  `json:"completedDateTime"`
	DeclinedDateTime      string                  `json:"declinedDateTime"`
	VoidedDateTime        string                  `json:"voidedDateTime"`
	StatusChangedDateTime string                  `json:"statusChangedDateTime"`
	ExpireDateTime        string                  `json:"expireDateTime"`
	VoidedReason          string                  `json:"voidedReason"`
	CustomFields          *docusignCustomFieldSet `json:"customFields"`
}

// docusignCustomFieldSet is DocuSign's customFields object; list custom fields are not read.
type docusignCustomFieldSet struct {
	TextCustomFields []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"textCustomFields"`
}

// decodeEnvelope reads one envelope resource and requires a GUID ID and a status.
func decodeEnvelope(body []byte) (Envelope, error) {
	var resource docusignEnvelopeResource
	if err := json.Unmarshal(body, &resource); err != nil {
		return Envelope{}, errDocuSignResponseMalformed
	}
	return resource.convert()
}

func (resource docusignEnvelopeResource) convert() (Envelope, error) {
	status := EnvelopeStatus(strings.ToLower(resource.Status))
	if !isDocuSignGUID(resource.EnvelopeID) || !envelopeStatusPattern.MatchString(string(status)) {
		return Envelope{}, errDocuSignResponseMalformed
	}
	envelope := Envelope{
		EnvelopeID: lowercaseGUID(resource.EnvelopeID), Status: status, EmailSubject: resource.EmailSubject,
		VoidedReason: resource.VoidedReason, CustomFields: resource.CustomFields.textFields(),
	}
	timestamps := []struct {
		value  string
		target **time.Time
	}{
		{resource.CreatedDateTime, &envelope.CreatedAt}, {resource.SentDateTime, &envelope.SentAt},
		{resource.DeliveredDateTime, &envelope.DeliveredAt}, {resource.CompletedDateTime, &envelope.CompletedAt},
		{resource.DeclinedDateTime, &envelope.DeclinedAt}, {resource.VoidedDateTime, &envelope.VoidedAt},
		{resource.StatusChangedDateTime, &envelope.StatusChangedAt}, {resource.ExpireDateTime, &envelope.ExpiresAt},
	}
	for _, timestamp := range timestamps {
		parsed, err := parseOptionalDocuSignTime(timestamp.value)
		if err != nil {
			return Envelope{}, err
		}
		*timestamp.target = parsed
	}
	return envelope, nil
}

// textFields returns the named text custom fields in DocuSign's order.
func (fields *docusignCustomFieldSet) textFields() []EnvelopeCustomField {
	if fields == nil {
		return nil
	}
	var converted []EnvelopeCustomField
	for _, field := range fields.TextCustomFields {
		if field.Name != "" {
			converted = append(converted, EnvelopeCustomField{Name: field.Name, Value: field.Value})
		}
	}
	return converted
}
