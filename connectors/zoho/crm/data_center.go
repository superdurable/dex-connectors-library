// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"errors"
	"net/url"
)

// Authorization method IDs, one per Zoho data center that serves Zoho CRM. Each method signs in
// through that data center's Zoho Accounts server, and every request goes to its zohoapis host.
const (
	// USDataCenterAuthMethodID is the United States data center: accounts.zoho.com and www.zohoapis.com.
	USDataCenterAuthMethodID = "zoho-us-oauth"
	// EUDataCenterAuthMethodID is the European Union data center: accounts.zoho.eu and www.zohoapis.eu.
	EUDataCenterAuthMethodID = "zoho-eu-oauth"
	// INDataCenterAuthMethodID is the India data center: accounts.zoho.in and www.zohoapis.in.
	INDataCenterAuthMethodID = "zoho-in-oauth"
	// AUDataCenterAuthMethodID is the Australia data center: accounts.zoho.com.au and www.zohoapis.com.au.
	AUDataCenterAuthMethodID = "zoho-au-oauth"
	// JPDataCenterAuthMethodID is the Japan data center: accounts.zoho.jp and www.zohoapis.jp.
	JPDataCenterAuthMethodID = "zoho-jp-oauth"
	// CADataCenterAuthMethodID is the Canada data center: accounts.zohocloud.ca and www.zohoapis.ca.
	CADataCenterAuthMethodID = "zoho-ca-oauth"
	// SADataCenterAuthMethodID is the Saudi Arabia data center: accounts.zoho.sa and www.zohoapis.sa.
	SADataCenterAuthMethodID = "zoho-sa-oauth"
)

// zohoAPIEnvironmentPrefixes are the hosts Zoho names in api_domain: production, sandbox, and developer.
var zohoAPIEnvironmentPrefixes = []string{"www.", "sandbox.", "developer."}

// DataCenter is one Zoho data center a connection can authorize in. Zoho keeps each account's data
// in the data center where it registered, so the authorization server and the Zoho CRM API host
// must both belong to it.
type DataCenter struct {
	// AuthMethodID is the manifest authorization method that selects this data center.
	AuthMethodID string
	// Name is the data center's name, such as European Union.
	Name string
	// AccountsURL is the Zoho Accounts server that issues and refreshes tokens, such as https://accounts.zoho.eu.
	AccountsURL string
	// APIDomainSuffix is the zohoapis domain of the data center, such as zohoapis.eu. Production is
	// served from www.{suffix}, and a sandbox or developer organization from sandbox.{suffix} or
	// developer.{suffix}.
	APIDomainSuffix string
}

// DataCenters returns every data center the connector supports, in the manifest's method order.
// Zoho's Zoho CRM v8 SDK names a zohoapis host for each, and Zoho Accounts lists each in
// https://accounts.zoho.com/oauth/serverinfo. China (zohoapis.com.cn) is a separately operated
// service outside that list; Singapore, the United Arab Emirates, and the United Kingdom have a
// Zoho Accounts server but no documented Zoho CRM API host, so none of them is offered.
func DataCenters() []DataCenter {
	return []DataCenter{
		{AuthMethodID: USDataCenterAuthMethodID, Name: "United States", AccountsURL: "https://accounts.zoho.com", APIDomainSuffix: "zohoapis.com"},
		{AuthMethodID: EUDataCenterAuthMethodID, Name: "European Union", AccountsURL: "https://accounts.zoho.eu", APIDomainSuffix: "zohoapis.eu"},
		{AuthMethodID: INDataCenterAuthMethodID, Name: "India", AccountsURL: "https://accounts.zoho.in", APIDomainSuffix: "zohoapis.in"},
		{AuthMethodID: AUDataCenterAuthMethodID, Name: "Australia", AccountsURL: "https://accounts.zoho.com.au", APIDomainSuffix: "zohoapis.com.au"},
		{AuthMethodID: JPDataCenterAuthMethodID, Name: "Japan", AccountsURL: "https://accounts.zoho.jp", APIDomainSuffix: "zohoapis.jp"},
		{AuthMethodID: CADataCenterAuthMethodID, Name: "Canada", AccountsURL: "https://accounts.zohocloud.ca", APIDomainSuffix: "zohoapis.ca"},
		{AuthMethodID: SADataCenterAuthMethodID, Name: "Saudi Arabia", AccountsURL: "https://accounts.zoho.sa", APIDomainSuffix: "zohoapis.sa"},
	}
}

// DataCenterForAuthMethod returns the data center an authorization method selects, or false for
// an unknown method.
func DataCenterForAuthMethod(authMethodID string) (DataCenter, bool) {
	for _, dataCenter := range DataCenters() {
		if dataCenter.AuthMethodID == authMethodID {
			return dataCenter, true
		}
	}
	return DataCenter{}, false
}

// ProductionAPIDomain returns the data center's production Zoho CRM API host, such as https://www.zohoapis.eu.
func (dataCenter DataCenter) ProductionAPIDomain() string {
	return "https://www." + dataCenter.APIDomainSuffix
}

// ValidateAPIDomain accepts the api_domain Zoho returned at authorization or refresh only when it
// is exactly https://www., https://sandbox., or https://developer. followed by this data center's
// zohoapis domain, without a port, path, query, fragment, or user information, so a token is never
// sent to another host. It returns the host without a trailing slash.
func (dataCenter DataCenter) ValidateAPIDomain(apiDomain string) (string, error) {
	parsed, err := url.Parse(apiDomain)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("Zoho CRM api_domain must be an https zohoapis host of the connection's data center")
	}
	for _, prefix := range zohoAPIEnvironmentPrefixes {
		if parsed.Host == prefix+dataCenter.APIDomainSuffix {
			return "https://" + parsed.Host, nil
		}
	}
	return "", errors.New("Zoho CRM api_domain is not a zohoapis host of the connection's data center")
}
