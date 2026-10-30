// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

// Authorization method IDs, one per Zoho data center that serves Zoho Desk. Each method signs in
// through that data center's Zoho Accounts server, and every request goes to its Zoho Desk host.
const (
	// USDataCenterAuthMethodID is the United States data center: accounts.zoho.com and desk.zoho.com.
	USDataCenterAuthMethodID = "zoho-us-oauth"
	// EUDataCenterAuthMethodID is the European Union data center: accounts.zoho.eu and desk.zoho.eu.
	EUDataCenterAuthMethodID = "zoho-eu-oauth"
	// INDataCenterAuthMethodID is the India data center: accounts.zoho.in and desk.zoho.in.
	INDataCenterAuthMethodID = "zoho-in-oauth"
	// AUDataCenterAuthMethodID is the Australia data center: accounts.zoho.com.au and desk.zoho.com.au.
	AUDataCenterAuthMethodID = "zoho-au-oauth"
	// JPDataCenterAuthMethodID is the Japan data center: accounts.zoho.jp and desk.zoho.jp.
	JPDataCenterAuthMethodID = "zoho-jp-oauth"
	// CADataCenterAuthMethodID is the Canada data center: accounts.zohocloud.ca and desk.zohocloud.ca.
	CADataCenterAuthMethodID = "zoho-ca-oauth"
	// SADataCenterAuthMethodID is the Saudi Arabia data center: accounts.zoho.sa and desk.zoho.sa.
	SADataCenterAuthMethodID = "zoho-sa-oauth"
	// SGDataCenterAuthMethodID is the Singapore data center: accounts.zoho.sg and desk.zoho.sg.
	SGDataCenterAuthMethodID = "zoho-sg-oauth"
	// AEDataCenterAuthMethodID is the United Arab Emirates data center: accounts.zoho.ae and desk.zoho.ae.
	AEDataCenterAuthMethodID = "zoho-ae-oauth"
)

// DataCenter is one Zoho data center a connection can authorize in. Zoho keeps each account's data
// in the data center where it registered, so the authorization server and the Zoho Desk host must
// both belong to it.
type DataCenter struct {
	// AuthMethodID is the manifest authorization method that selects this data center.
	AuthMethodID string
	// Name is the data center's name, such as European Union.
	Name string
	// AccountsURL is the Zoho Accounts server that issues and refreshes tokens, such as https://accounts.zoho.eu.
	AccountsURL string
	// DeskURL is the Zoho Desk host that serves the API below /api/v1, such as https://desk.zoho.eu.
	DeskURL string
	// StudioOrganizationsCommandID is the setup command that lists organizations on DeskURL.
	StudioOrganizationsCommandID string
}

// DataCenters returns every data center the connector supports, in the manifest's method order.
// Zoho Desk documents an API host for each, and Zoho Accounts lists each in
// https://accounts.zoho.com/oauth/serverinfo. China (desk.zoho.com.cn) is a separately operated
// service outside that list, and Zoho Desk documents no United Kingdom host, so neither is offered.
func DataCenters() []DataCenter {
	return []DataCenter{
		{AuthMethodID: USDataCenterAuthMethodID, Name: "United States", AccountsURL: "https://accounts.zoho.com", DeskURL: "https://desk.zoho.com", StudioOrganizationsCommandID: "listOrganizationsUS"},
		{AuthMethodID: EUDataCenterAuthMethodID, Name: "European Union", AccountsURL: "https://accounts.zoho.eu", DeskURL: "https://desk.zoho.eu", StudioOrganizationsCommandID: "listOrganizationsEU"},
		{AuthMethodID: INDataCenterAuthMethodID, Name: "India", AccountsURL: "https://accounts.zoho.in", DeskURL: "https://desk.zoho.in", StudioOrganizationsCommandID: "listOrganizationsIN"},
		{AuthMethodID: AUDataCenterAuthMethodID, Name: "Australia", AccountsURL: "https://accounts.zoho.com.au", DeskURL: "https://desk.zoho.com.au", StudioOrganizationsCommandID: "listOrganizationsAU"},
		{AuthMethodID: JPDataCenterAuthMethodID, Name: "Japan", AccountsURL: "https://accounts.zoho.jp", DeskURL: "https://desk.zoho.jp", StudioOrganizationsCommandID: "listOrganizationsJP"},
		{AuthMethodID: CADataCenterAuthMethodID, Name: "Canada", AccountsURL: "https://accounts.zohocloud.ca", DeskURL: "https://desk.zohocloud.ca", StudioOrganizationsCommandID: "listOrganizationsCA"},
		{AuthMethodID: SADataCenterAuthMethodID, Name: "Saudi Arabia", AccountsURL: "https://accounts.zoho.sa", DeskURL: "https://desk.zoho.sa", StudioOrganizationsCommandID: "listOrganizationsSA"},
		{AuthMethodID: SGDataCenterAuthMethodID, Name: "Singapore", AccountsURL: "https://accounts.zoho.sg", DeskURL: "https://desk.zoho.sg", StudioOrganizationsCommandID: "listOrganizationsSG"},
		{AuthMethodID: AEDataCenterAuthMethodID, Name: "United Arab Emirates", AccountsURL: "https://accounts.zoho.ae", DeskURL: "https://desk.zoho.ae", StudioOrganizationsCommandID: "listOrganizationsAE"},
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
