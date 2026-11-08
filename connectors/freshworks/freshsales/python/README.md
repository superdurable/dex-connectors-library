# Freshsales Python connector

This package implements `listContactViews` and `getContact`. Its requests, branches, retry decisions, and typed outputs replay the Go-recorded provider scenarios. View results contain IDs and names; contact results contain ID, name, and email. Custom fields and links stay outside Flow results.

Sign in to [Freshsales CRM](https://app.freshworks.com/), open the profile picture > Profile Settings > API Settings, and copy your personal API key into the Dex secret `api_key`. Copy the Bundle alias URL ending in `/crm/sales/api` into `baseUrl`. See the [connector README](../README.md) and [runnable example](examples/contact_view_discovery/README.md). Live provider authorization remains unverified.
