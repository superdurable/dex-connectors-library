// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listEnvelopeRecipientsOperationID = "listEnvelopeRecipients"

// RecipientType is the DocuSign recipient collection a recipient belongs to.
type RecipientType string

const (
	// RecipientTypeSigner signs the documents.
	RecipientTypeSigner RecipientType = "signer"
	// RecipientTypeCarbonCopy receives a copy of the completed envelope.
	RecipientTypeCarbonCopy RecipientType = "carbonCopy"
	// RecipientTypeCertifiedDelivery must open the envelope before routing continues.
	RecipientTypeCertifiedDelivery RecipientType = "certifiedDelivery"
	// RecipientTypeInPersonSigner signs in person with a host.
	RecipientTypeInPersonSigner RecipientType = "inPersonSigner"
	// RecipientTypeAgent names the recipients of a later routing order.
	RecipientTypeAgent RecipientType = "agent"
	// RecipientTypeEditor may change the envelope's later recipients and fields.
	RecipientTypeEditor RecipientType = "editor"
	// RecipientTypeIntermediary forwards the envelope to another recipient.
	RecipientTypeIntermediary RecipientType = "intermediary"
	// RecipientTypeWitness witnesses a signer's signature.
	RecipientTypeWitness RecipientType = "witness"
)

// ListEnvelopeRecipientsInput names the envelope whose recipients to list.
type ListEnvelopeRecipientsInput struct {
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
}

// EnvelopeRecipients is one envelope's recipients sorted by routing order and then recipient ID.
type EnvelopeRecipients struct {
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
	// CurrentRoutingOrder is the routing order DocuSign is waiting on; zero when DocuSign did not say.
	CurrentRoutingOrder int `json:"currentRoutingOrder,omitempty"`
	// Recipients lists every recipient of the types RecipientType names.
	Recipients []Recipient `json:"recipients"`
}

// Recipient is one envelope recipient's identity, place in the routing order, and progress. Access
// codes and authentication details are never read.
type Recipient struct {
	// RecipientID is the recipient's ID within the envelope, such as 1.
	RecipientID string `json:"recipientId"`
	// Type is the recipient's collection, such as signer or carbonCopy.
	Type RecipientType `json:"type"`
	// Name is the recipient's name.
	Name string `json:"name,omitempty"`
	// Email is the recipient's email address.
	Email string `json:"email,omitempty"`
	// RoleName is the template role the recipient fills, if any.
	RoleName string `json:"roleName,omitempty"`
	// RoutingOrder is the recipient's routing order; recipients with the same order receive it together.
	RoutingOrder int `json:"routingOrder"`
	// Status is DocuSign's recipient status, such as created, sent, delivered, completed, or declined.
	Status string `json:"status"`
	// SentAt is when DocuSign sent the envelope to this recipient.
	SentAt *time.Time `json:"sentAt,omitempty"`
	// DeliveredAt is when the recipient opened it.
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
	// SignedAt is when the recipient finished.
	SignedAt *time.Time `json:"signedAt,omitempty"`
	// DeclinedAt is when the recipient declined.
	DeclinedAt *time.Time `json:"declinedAt,omitempty"`
	// DeclinedReason is the reason the recipient gave for declining.
	DeclinedReason string `json:"declinedReason,omitempty"`
}

// docusignRecipient is the part of every recipient object the connector reads.
type docusignRecipient struct {
	RecipientID       string `json:"recipientId"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	RoleName          string `json:"roleName"`
	RoutingOrder      string `json:"routingOrder"`
	Status            string `json:"status"`
	SentDateTime      string `json:"sentDateTime"`
	DeliveredDateTime string `json:"deliveredDateTime"`
	SignedDateTime    string `json:"signedDateTime"`
	DeclinedDateTime  string `json:"declinedDateTime"`
	DeclinedReason    string `json:"declinedReason"`
}

// ListEnvelopeRecipientsOperation implements the listEnvelopeRecipients Query.
type ListEnvelopeRecipientsOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ListEnvelopeRecipientsOperation) Definition() sdkgo.QueryDefinition {
	return ListEnvelopeRecipientsDefinition
}

// Invoke lists the envelope's recipients. A 429 or 5xx returns Retry.
func (operation ListEnvelopeRecipientsOperation) Invoke(call sdkgo.Call, input ListEnvelopeRecipientsInput) sdkgo.QueryAttempt[EnvelopeRecipients] {
	client := operation.client
	if err := validateEnvelopeID(input.EnvelopeID); err != nil {
		return sdkgo.NewQueryBranch(ListEnvelopeRecipientsBranchDefect, EnvelopeRecipients{}, docusignFailurePointer(listEnvelopeRecipientsOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, sessionFailure := client.openSession(call, listEnvelopeRecipientsOperationID)
	if sessionFailure != nil {
		return querySessionFailure[EnvelopeRecipients](sessionFailure, ListEnvelopeRecipientsBranchProviderRejected, ListEnvelopeRecipientsBranchInvalidResponse, ListEnvelopeRecipientsBranchDefect)
	}
	response, err := client.sendAPIRequest(call, &session, docusignRequest{
		method: http.MethodGet, path: "/envelopes/" + url.PathEscape(input.EnvelopeID) + "/recipients",
	}, nil)
	receipt := client.receipt(lowercaseGUID(input.EnvelopeID), "")
	switch {
	case errors.Is(err, errDocuSignResponseTooLarge):
		return sdkgo.NewQueryBranch(ListEnvelopeRecipientsBranchInvalidResponse, EnvelopeRecipients{}, docusignFailurePointer(listEnvelopeRecipientsOperationID, sdkgo.FailureResponseTooLarge, err.Error()), receipt)
	case err != nil:
		return sdkgo.NewQueryRetry[EnvelopeRecipients](docusignFailure(listEnvelopeRecipientsOperationID, sdkgo.FailureTransport, "DocuSign could not be reached to list the recipients"), 0)
	case response.statusCode != http.StatusOK:
		outcome := client.classifyDocuSignFailure(listEnvelopeRecipientsOperationID, response)
		receipt = client.receipt(lowercaseGUID(input.EnvelopeID), outcome.errorCode)
		switch {
		case outcome.isRetry:
			return sdkgo.NewQueryRetry[EnvelopeRecipients](outcome.failure, outcome.retryAfter)
		case outcome.isNotFound:
			return sdkgo.NewQueryBranch(ListEnvelopeRecipientsBranchNotFound, EnvelopeRecipients{}, &outcome.failure, receipt)
		default:
			return sdkgo.NewQueryBranch(ListEnvelopeRecipientsBranchProviderRejected, EnvelopeRecipients{}, &outcome.failure, receipt)
		}
	}
	recipients, err := decodeEnvelopeRecipients(response.body, lowercaseGUID(input.EnvelopeID))
	if err != nil {
		return sdkgo.NewQueryBranch(ListEnvelopeRecipientsBranchInvalidResponse, EnvelopeRecipients{}, docusignFailurePointer(listEnvelopeRecipientsOperationID, sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListEnvelopeRecipientsBranchListed, recipients, nil, receipt)
}

// decodeEnvelopeRecipients flattens DocuSign's per-type recipient arrays into one routing-ordered list.
func decodeEnvelopeRecipients(body []byte, envelopeID string) (EnvelopeRecipients, error) {
	var decoded struct {
		CurrentRoutingOrder string              `json:"currentRoutingOrder"`
		Signers             []docusignRecipient `json:"signers"`
		CarbonCopies        []docusignRecipient `json:"carbonCopies"`
		CertifiedDeliveries []docusignRecipient `json:"certifiedDeliveries"`
		InPersonSigners     []docusignRecipient `json:"inPersonSigners"`
		Agents              []docusignRecipient `json:"agents"`
		Editors             []docusignRecipient `json:"editors"`
		Intermediaries      []docusignRecipient `json:"intermediaries"`
		Witnesses           []docusignRecipient `json:"witnesses"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return EnvelopeRecipients{}, errDocuSignResponseMalformed
	}
	currentRoutingOrder, err := parseOptionalRoutingOrder(decoded.CurrentRoutingOrder)
	if err != nil {
		return EnvelopeRecipients{}, err
	}
	result := EnvelopeRecipients{EnvelopeID: envelopeID, CurrentRoutingOrder: currentRoutingOrder, Recipients: []Recipient{}}
	collections := []struct {
		recipientType RecipientType
		recipients    []docusignRecipient
	}{
		{RecipientTypeSigner, decoded.Signers}, {RecipientTypeCarbonCopy, decoded.CarbonCopies},
		{RecipientTypeCertifiedDelivery, decoded.CertifiedDeliveries}, {RecipientTypeInPersonSigner, decoded.InPersonSigners},
		{RecipientTypeAgent, decoded.Agents}, {RecipientTypeEditor, decoded.Editors},
		{RecipientTypeIntermediary, decoded.Intermediaries}, {RecipientTypeWitness, decoded.Witnesses},
	}
	for _, collection := range collections {
		for _, recipient := range collection.recipients {
			converted, err := recipient.convert(collection.recipientType)
			if err != nil {
				return EnvelopeRecipients{}, err
			}
			result.Recipients = append(result.Recipients, converted)
		}
	}
	slices.SortStableFunc(result.Recipients, func(left Recipient, right Recipient) int {
		return cmp.Or(cmp.Compare(left.RoutingOrder, right.RoutingOrder), compareRecipientIDs(left.RecipientID, right.RecipientID))
	})
	return result, nil
}

func (recipient docusignRecipient) convert(recipientType RecipientType) (Recipient, error) {
	routingOrder, err := parseOptionalRoutingOrder(recipient.RoutingOrder)
	if err != nil || recipient.RecipientID == "" || recipient.Status == "" {
		return Recipient{}, errDocuSignResponseMalformed
	}
	converted := Recipient{
		RecipientID: recipient.RecipientID, Type: recipientType, Name: recipient.Name, Email: recipient.Email,
		RoleName: recipient.RoleName, RoutingOrder: routingOrder, Status: strings.ToLower(recipient.Status),
		DeclinedReason: recipient.DeclinedReason,
	}
	timestamps := []struct {
		value  string
		target **time.Time
	}{
		{recipient.SentDateTime, &converted.SentAt}, {recipient.DeliveredDateTime, &converted.DeliveredAt},
		{recipient.SignedDateTime, &converted.SignedAt}, {recipient.DeclinedDateTime, &converted.DeclinedAt},
	}
	for _, timestamp := range timestamps {
		if *timestamp.target, err = parseOptionalDocuSignTime(timestamp.value); err != nil {
			return Recipient{}, err
		}
	}
	return converted, nil
}

// parseOptionalRoutingOrder reads DocuSign's string routing order; blank is zero.
func parseOptionalRoutingOrder(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	routingOrder, err := strconv.Atoi(value)
	if err != nil || routingOrder < 0 {
		return 0, errDocuSignResponseMalformed
	}
	return routingOrder, nil
}

// compareRecipientIDs orders numeric recipient IDs numerically and others lexically after them.
func compareRecipientIDs(left string, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	if leftErr == nil && rightErr == nil {
		return cmp.Compare(leftNumber, rightNumber)
	}
	if (leftErr == nil) != (rightErr == nil) {
		return map[bool]int{true: -1, false: 1}[leftErr == nil]
	}
	return strings.Compare(left, right)
}
