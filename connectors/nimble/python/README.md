# Nimble Python connector

This package implements `listContactIDs` and `getContact`. Its requests, branches, retry decisions, and typed outputs replay the Go-recorded provider scenarios. Results contain contact IDs and selected name and email fields; custom fields and links stay outside Flow results.

Sign in to [Nimble CRM](https://app.nimble.com/) as an administrator, open **Settings > API Token > Generate New Token**, name the token and choose Contacts scope, then copy it into Dex secret `api_key`. An administrator may need to grant API access under **Settings > Users**. See the [connector README](../README.md) and [runnable example](examples/contact_id_discovery/README.md). Live provider authorization remains unverified.
