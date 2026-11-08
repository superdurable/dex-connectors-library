# Freshsales TypeScript connector

This package implements `listContactViews` and `getContact` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. View results contain IDs and names; contact results contain ID, name, and email. Custom fields and links remain outside Flow results.

Sign in to [Freshsales CRM](https://app.freshworks.com/), open the profile picture > Profile Settings > API Settings, and copy your personal API key into Dex secret `api_key`. Copy the Bundle alias URL ending in `/crm/sales/api` into `baseUrl`. See the [connector README](../README.md) and [contact discovery example](examples/contact-view-discovery/README.md).

JavaScript numbers cannot represent provider numeric IDs above the safe integer limit exactly; such responses select `invalidResponse`. Live Freshsales authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
