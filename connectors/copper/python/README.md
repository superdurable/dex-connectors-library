# Copper Python connector

This package implements `listLeadStatuses` and `getLead`, replaying Go-recorded provider requests, branches, retries, and exact outputs. It returns lead status IDs and names, and a lead's ID, name, status, and company name. Custom fields and email addresses stay outside Flow results.

Sign in as an admin to [Copper](https://app.copper.com/), open **Settings > Integrations > API Keys**, generate a key, and save it in Dex secret `api_key`. Save the generating user's Copper login email in `user_email`. Leave `baseUrl` blank for the Developer API v1 default. See the [connector README](../README.md) and [runnable example](examples/lead_status_discovery/README.md). Live provider authorization remains unverified.
