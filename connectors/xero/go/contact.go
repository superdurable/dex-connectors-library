// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"errors"
	"strings"
	"time"
)

// ContactStatus is Xero's own contact status value.
type ContactStatus string

const (
	// ContactStatusActive is a contact that can be used in transactions.
	ContactStatusActive ContactStatus = "ACTIVE"
	// ContactStatusArchived is a contact that can no longer be used in transactions.
	ContactStatusArchived ContactStatus = "ARCHIVED"
	// ContactStatusGDPRRequest is a contact under a GDPR erasure request, which can no longer be used.
	ContactStatusGDPRRequest ContactStatus = "GDPRREQUEST"
)

// Contact is one Xero customer or supplier. Status and flags keep Xero's values; a status
// Xero adds later is passed through unchanged.
type Contact struct {
	// ContactID is Xero's stable contact identifier, a UUID; use it to raise an invoice.
	ContactID string `json:"contactId"`
	// Name is the contact or organisation name, which Xero may stop treating as unique.
	Name string `json:"name"`
	// FirstName is the contact person's first name, when set.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the contact person's last name, when set.
	LastName string `json:"lastName,omitempty"`
	// EmailAddress is the contact person's email address, when set.
	EmailAddress string `json:"emailAddress,omitempty"`
	// ContactNumber is an identifier from an external system, shown as Contact Code in Xero.
	ContactNumber string `json:"contactNumber,omitempty"`
	// AccountNumber is a user-defined account number.
	AccountNumber string `json:"accountNumber,omitempty"`
	// Status is ACTIVE, ARCHIVED, or GDPRREQUEST.
	Status ContactStatus `json:"status"`
	// IsCustomer reports that the contact has sales invoices.
	IsCustomer bool `json:"isCustomer,omitempty"`
	// IsSupplier reports that the contact has bills.
	IsSupplier bool `json:"isSupplier,omitempty"`
	// DefaultCurrency is the ISO 4217 currency Xero suggests for the contact's invoices, when set.
	DefaultCurrency string `json:"defaultCurrency,omitempty"`
	// UpdatedAt is Xero's last-modified instant in UTC, or zero when Xero omits it.
	UpdatedAt time.Time `json:"updatedAt"`
}

// ContactPage is one bounded page of contacts.
type ContactPage struct {
	// Contacts are the contacts on this page in Xero's order, by last modification then ID.
	Contacts []Contact `json:"contacts"`
	// Page is the 1-based page number that was read.
	Page int `json:"page"`
	// PageSize is the requested page size.
	PageSize int `json:"pageSize"`
	// PageCount is Xero's total page count, or zero when Xero omits pagination.
	PageCount int `json:"pageCount,omitempty"`
	// ItemCount is Xero's total number of matching contacts, or zero when Xero omits pagination.
	ItemCount int `json:"itemCount,omitempty"`
	// HasMorePages reports that Page+1 may hold more contacts.
	HasMorePages bool `json:"hasMorePages"`
}

// contactWire is the subset of a Xero Contact element the connector reads.
type contactWire struct {
	ContactID       string   `json:"ContactID"`
	ContactNumber   string   `json:"ContactNumber"`
	AccountNumber   string   `json:"AccountNumber"`
	ContactStatus   string   `json:"ContactStatus"`
	Name            string   `json:"Name"`
	FirstName       string   `json:"FirstName"`
	LastName        string   `json:"LastName"`
	EmailAddress    string   `json:"EmailAddress"`
	IsSupplier      wireBool `json:"IsSupplier"`
	IsCustomer      wireBool `json:"IsCustomer"`
	DefaultCurrency string   `json:"DefaultCurrency"`
	UpdatedDateUTC  string   `json:"UpdatedDateUTC"`
}

// contactPageWire is a GET Contacts response.
type contactPageWire struct {
	Pagination *pageWire      `json:"pagination"`
	Contacts   *[]contactWire `json:"Contacts"`
}

func decodeContact(wire contactWire) (Contact, error) {
	if !uuidPattern.MatchString(wire.ContactID) {
		return Contact{}, errors.New("contact has no ContactID")
	}
	updatedAt, err := timestampFromWire(wire.UpdatedDateUTC)
	if err != nil {
		return Contact{}, errors.New("contact has an invalid UpdatedDateUTC")
	}
	return Contact{
		ContactID: strings.ToLower(wire.ContactID), Name: wire.Name, FirstName: wire.FirstName, LastName: wire.LastName,
		EmailAddress: wire.EmailAddress, ContactNumber: wire.ContactNumber, AccountNumber: wire.AccountNumber,
		Status: ContactStatus(wire.ContactStatus), IsCustomer: bool(wire.IsCustomer), IsSupplier: bool(wire.IsSupplier),
		DefaultCurrency: wire.DefaultCurrency, UpdatedAt: updatedAt,
	}, nil
}

func decodeContacts(wires []contactWire) ([]Contact, error) {
	contacts := make([]Contact, 0, len(wires))
	for _, wire := range wires {
		contact, err := decodeContact(wire)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, nil
}
