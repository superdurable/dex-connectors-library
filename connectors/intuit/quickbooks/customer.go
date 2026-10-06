// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Customer is one QuickBooks Online customer. Amounts are exact Decimals in the customer's currency.
type Customer struct {
	// CustomerID is QuickBooks's Id for the customer, a decimal string such as 58.
	CustomerID string `json:"customerId"`
	// SyncToken is the version QuickBooks requires for an update of this customer.
	SyncToken string `json:"syncToken,omitempty"`
	// DisplayName is the name QuickBooks shows; it is unique across customers, vendors, and employees.
	DisplayName string `json:"displayName"`
	// GivenName is the contact's first name, when set.
	GivenName string `json:"givenName,omitempty"`
	// FamilyName is the contact's last name, when set.
	FamilyName string `json:"familyName,omitempty"`
	// CompanyName is the customer's company, when set.
	CompanyName string `json:"companyName,omitempty"`
	// EmailAddress is the primary email address, when set. QuickBooks does not require it to be unique.
	EmailAddress string `json:"emailAddress,omitempty"`
	// Phone is the primary phone number exactly as entered, when set.
	Phone string `json:"phone,omitempty"`
	// IsActive is false for a customer made inactive, which QuickBooks treats as deleted.
	IsActive bool `json:"isActive"`
	// Balance is the customer's open balance, when QuickBooks returns it.
	Balance Decimal `json:"balance,omitempty"`
	// CurrencyCode is the customer's ISO 4217 currency, when QuickBooks returns it.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// CreatedAt is when the customer was created, in UTC.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// UpdatedAt is when the customer last changed, in UTC.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// referenceWire is QuickBooks's ReferenceType, such as CustomerRef or CurrencyRef.
type referenceWire struct {
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

// emailAddressWire is QuickBooks's EmailAddress type.
type emailAddressWire struct {
	Address string `json:"Address"`
}

// phoneNumberWire is QuickBooks's TelephoneNumber type.
type phoneNumberWire struct {
	FreeFormNumber string `json:"FreeFormNumber"`
}

// metadataWire is QuickBooks's ModificationMetaData.
type metadataWire struct {
	CreateTime      string `json:"CreateTime"`
	LastUpdatedTime string `json:"LastUpdatedTime"`
}

type customerWire struct {
	ID               string            `json:"Id"`
	SyncToken        string            `json:"SyncToken"`
	DisplayName      string            `json:"DisplayName"`
	GivenName        string            `json:"GivenName"`
	FamilyName       string            `json:"FamilyName"`
	CompanyName      string            `json:"CompanyName"`
	PrimaryEmailAddr *emailAddressWire `json:"PrimaryEmailAddr"`
	PrimaryPhone     *phoneNumberWire  `json:"PrimaryPhone"`
	Active           *bool             `json:"Active"`
	Balance          wireDecimal       `json:"Balance"`
	CurrencyRef      *referenceWire    `json:"CurrencyRef"`
	MetaData         *metadataWire     `json:"MetaData"`
}

// decodeCustomer validates one customer element; QuickBooks omits Active for an active customer.
func decodeCustomer(wire customerWire) (Customer, error) {
	if !entityIDPattern.MatchString(wire.ID) {
		return Customer{}, errors.New("customer has no valid Id")
	}
	if strings.TrimSpace(wire.DisplayName) == "" {
		return Customer{}, errors.New("customer has no DisplayName")
	}
	customer := Customer{
		CustomerID: wire.ID, SyncToken: wire.SyncToken, DisplayName: wire.DisplayName, GivenName: wire.GivenName,
		FamilyName: wire.FamilyName, CompanyName: wire.CompanyName, IsActive: wire.Active == nil || *wire.Active,
		Balance: wire.Balance.value,
	}
	if wire.PrimaryEmailAddr != nil {
		customer.EmailAddress = strings.TrimSpace(wire.PrimaryEmailAddr.Address)
	}
	if wire.PrimaryPhone != nil {
		customer.Phone = wire.PrimaryPhone.FreeFormNumber
	}
	if wire.CurrencyRef != nil {
		customer.CurrencyCode = wire.CurrencyRef.Value
	}
	createdAt, updatedAt, err := decodeMetadataTimes(wire.MetaData)
	if err != nil {
		return Customer{}, fmt.Errorf("customer %w", err)
	}
	customer.CreatedAt, customer.UpdatedAt = createdAt, updatedAt
	return customer, nil
}

// decodeMetadataTimes reads QuickBooks's RFC 3339 times, which carry the company's UTC offset, as UTC.
func decodeMetadataTimes(metadata *metadataWire) (*time.Time, *time.Time, error) {
	if metadata == nil {
		return nil, nil, nil
	}
	createdAt, err := parseOptionalTimestamp(metadata.CreateTime)
	if err != nil {
		return nil, nil, errors.New("has an invalid MetaData.CreateTime")
	}
	updatedAt, err := parseOptionalTimestamp(metadata.LastUpdatedTime)
	if err != nil {
		return nil, nil, errors.New("has an invalid MetaData.LastUpdatedTime")
	}
	return createdAt, updatedAt, nil
}

func parseOptionalTimestamp(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	return &utc, nil
}
