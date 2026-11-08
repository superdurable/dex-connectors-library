// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package searchconsole

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListSitesInput requests properties visible to the connected Google user.
type ListSitesInput struct{}

// Site describes one Search Console property and the user's permission.
type Site struct {
	// SiteURL is a URL-prefix property or a sc-domain property identifier.
	SiteURL string `json:"siteUrl"`
	// PermissionLevel is the Google-issued permission for this user.
	PermissionLevel string `json:"permissionLevel"`
}

// SitesPage contains the bounded set returned by Search Console.
type SitesPage struct {
	// Sites are the accessible Search Console properties.
	Sites []Site `json:"sites"`
}

// ListSitesOperation reads the connected user's Search Console properties.
type ListSitesOperation struct{ client *Client }

// ListSites binds the properties Query to this client.
func (client *Client) ListSites() ListSitesOperation { return ListSitesOperation{client} }

// Definition returns the generated properties Query contract.
func (ListSitesOperation) Definition() sdkgo.QueryDefinition { return ListSitesDefinition }

// Invoke reads one bounded list of properties and projects only URLs and permissions.
func (operation ListSitesOperation) Invoke(call sdkgo.Call, _ ListSitesInput) sdkgo.QueryAttempt[SitesPage] {
	response := operation.client.get(call, "/webmasters/v3/sites")
	if errors.Is(response.err, errCredential) {
		return sitesBranch(ListSitesBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sitesRetry(sdkgo.FailureTransport, "Search Console request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sitesRetry(sdkgo.FailureRateLimit, "Google rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sitesRetry(sdkgo.FailureAvailability, "Search Console is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return sitesBranch(ListSitesBranchProviderRejected, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sitesBranch(ListSitesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Search Console response exceeded the configured limit")
	}
	if response.err != nil {
		return sitesRetry(sdkgo.FailureTransport, "Search Console response was interrupted", 0)
	}
	var wire struct {
		Entries []Site `json:"siteEntry"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || len(wire.Entries) > 1000 {
		return sitesBranch(ListSitesBranchInvalidResponse, sdkgo.FailureProtocol, "Google returned invalid property JSON")
	}
	page := SitesPage{Sites: make([]Site, 0, len(wire.Entries))}
	for _, site := range wire.Entries {
		if site.SiteURL == "" || !isPermissionLevel(site.PermissionLevel) {
			return sitesBranch(ListSitesBranchInvalidResponse, sdkgo.FailureProtocol, "Google returned incomplete property metadata")
		}
		page.Sites = append(page.Sites, site)
	}
	return sdkgo.NewQueryBranch(ListSitesBranchListed, page, nil, sdkgo.Receipt{})
}

func isPermissionLevel(permission string) bool {
	switch permission {
	case "siteOwner", "siteFullUser", "siteRestrictedUser", "siteUnverifiedUser":
		return true
	default:
		return false
	}
}

func sitesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SitesPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-search-console", Operation: "listSites", Message: message}
	return sdkgo.NewQueryBranch(branch, SitesPage{}, &failure, sdkgo.Receipt{})
}

func sitesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SitesPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-search-console", Operation: "listSites", Message: message}
	return sdkgo.NewQueryRetry[SitesPage](failure, delay)
}
