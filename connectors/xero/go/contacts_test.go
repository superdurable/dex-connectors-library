// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const otherContactID = "06638157-fdfa-47f4-91d0-875b5f5c18c6"

func TestFindContactByEmailUsesTheOptimisedFilterAndSelectsOneContact(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Contacts": []any{
			contactJSON(testContactID, "Accounts@CityAgency.example.com", "ACTIVE"),
			contactJSON(otherContactID, "accounts@cityagençy.example.com", "ACTIVE"),
		}})
	})
	result, err := sdkgo.RunQuery(newXeroDexContext("find"), newXeroClient(t, provider.URL).FindContactByEmail(), xeroConnection,
		xero.FindContactByEmailInput{EmailAddress: " accounts@cityagency.example.com "})
	require.NoError(t, err)
	require.Equal(t, xero.FindContactByEmailBranchFound, result.Branch, "an accent-insensitive Xero match with another address is dropped")
	require.Equal(t, "accounts@cityagency.example.com", result.Value.EmailAddress)
	require.NotNil(t, result.Value.Contact)
	contact := *result.Value.Contact
	require.Equal(t, testContactID, contact.ContactID)
	require.Equal(t, xero.ContactStatusActive, contact.Status)
	require.True(t, contact.IsCustomer, "Xero's string flags are decoded")
	require.False(t, contact.IsSupplier)
	require.Equal(t, time.Date(2018, 2, 15, 9, 12, 30, 940000000, time.UTC), contact.UpdatedAt)
	require.Equal(t, testContactID, result.Receipt.ProviderObjectID)

	request := provider.request(0)
	require.Equal(t, "/api.xro/2.0/Contacts", request.path)
	require.Equal(t, `EmailAddress=="accounts@cityagency.example.com"`, request.query.Get("where"))
	require.Equal(t, "1", request.query.Get("page"))
	require.Equal(t, "10", request.query.Get("pageSize"))
	require.Empty(t, request.query.Get("includeArchived"))
}

func TestFindContactByEmailDistinguishesNoneSeveralAndArchived(t *testing.T) {
	for _, test := range []struct {
		name             string
		contacts         []any
		includesArchived bool
		branch           sdkgo.BranchID
		matches          int
	}{
		{name: "none", contacts: []any{}, branch: xero.FindContactByEmailBranchNotFound},
		{name: "several", contacts: []any{
			contactJSON(testContactID, "accounts@cityagency.example.com", "ACTIVE"),
			contactJSON(otherContactID, "accounts@cityagency.example.com", "ACTIVE"),
		}, branch: xero.FindContactByEmailBranchAmbiguous, matches: 2},
		{name: "archived only", contacts: []any{contactJSON(testContactID, "accounts@cityagency.example.com", "ARCHIVED")},
			branch: xero.FindContactByEmailBranchNotFound},
		{name: "archived requested", contacts: []any{contactJSON(testContactID, "accounts@cityagency.example.com", "ARCHIVED")},
			includesArchived: true, branch: xero.FindContactByEmailBranchFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeValue(t, response, http.StatusOK, map[string]any{"Contacts": test.contacts})
			})
			result, err := sdkgo.RunQuery(newXeroDexContext("find-"+test.name), newXeroClient(t, provider.URL).FindContactByEmail(), xeroConnection,
				xero.FindContactByEmailInput{EmailAddress: "accounts@cityagency.example.com", IncludesArchived: test.includesArchived})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Len(t, result.Value.Matches, test.matches)
			if test.includesArchived {
				require.Equal(t, "true", provider.request(0).query.Get("includeArchived"))
			}
		})
	}
}

func TestFindContactByEmailRejectsAnAddressThatCouldEscapeTheFilter(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Contacts": []any{}})
	})
	client := newXeroClient(t, provider.URL)
	for _, address := range []string{"", "City Agency <accounts@cityagency.example.com>", `a"||Name!=""@example.com`, "a\\b@example.com", "not-an-address"} {
		result, err := sdkgo.RunQuery(newXeroDexContext("bad-email"), client.FindContactByEmail(), xeroConnection, xero.FindContactByEmailInput{EmailAddress: address})
		require.NoError(t, err)
		require.Equal(t, xero.FindContactByEmailBranchDefect, result.Branch, address)
	}
	require.Zero(t, provider.requestCount())
}

func TestListContactsSendsFiltersAndBoundsThePage(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Contacts": []any{
			contactJSON(testContactID, "a@example.com", "ACTIVE"), contactJSON(otherContactID, "b@example.com", "ARCHIVED"),
		}})
	})
	modifiedSince := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	client := newXeroClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newXeroDexContext("list"), client.ListContacts(), xeroConnection, xero.ListContactsInput{
		SearchTerm: " agency ", ContactIDs: []string{testContactID, otherContactID}, IncludesArchived: true,
		ModifiedSince: &modifiedSince, Page: 3, PageSize: 2,
	})
	require.NoError(t, err)
	require.Equal(t, xero.ListContactsBranchListed, result.Branch)
	require.Len(t, result.Value.Contacts, 2)
	require.Equal(t, xero.ContactStatusArchived, result.Value.Contacts[1].Status)
	require.True(t, result.Value.HasMorePages, "without Xero pagination a full page may be followed by another")
	request := provider.request(0)
	require.Equal(t, "agency", request.query.Get("searchTerm"))
	require.Equal(t, testContactID+","+otherContactID, request.query.Get("IDs"))
	require.Equal(t, "true", request.query.Get("includeArchived"))
	require.Equal(t, "3", request.query.Get("page"))
	require.Equal(t, "2", request.query.Get("pageSize"))
	require.Equal(t, "2026-09-01T00:00:00", request.header.Get("If-Modified-Since"))

	for name, input := range map[string]xero.ListContactsInput{
		"zero page size is default": {},
		"page size above the bound": {PageSize: 1000},
		"too many IDs":              {ContactIDs: make([]string, xero.MaxFilterValues+1)},
		"multi-line search":         {SearchTerm: "a\nb"},
	} {
		result, err := sdkgo.RunQuery(newXeroDexContext(name), client.ListContacts(), xeroConnection, input)
		require.NoError(t, err)
		if name == "zero page size is default" {
			require.Equal(t, xero.ListContactsBranchListed, result.Branch)
			require.Equal(t, "25", provider.request(1).query.Get("pageSize"))
			continue
		}
		require.Equal(t, xero.ListContactsBranchDefect, result.Branch, name)
	}
	require.Equal(t, 2, provider.requestCount())
}

func TestListContactsRejectsAPageWithoutAContactList(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{"Status":"OK"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"Contacts":[{"Name":"No ID"}]}`)
	})
	client := newXeroClient(t, provider.URL)
	for range 2 {
		result, err := sdkgo.RunQuery(newXeroDexContext("invalid"), client.ListContacts(), xeroConnection, xero.ListContactsInput{})
		require.NoError(t, err)
		require.Equal(t, xero.ListContactsBranchInvalidResponse, result.Branch)
	}
}
