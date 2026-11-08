# Insightly Python connector

This package implements `listContacts` and `getContact`. Its requests, branches, retry decisions, and typed outputs replay the Go-recorded provider scenarios. Results contain contact IDs, names, and email addresses; custom fields and links stay outside Flow results.

Sign in to [Insightly CRM](https://login.insightly.com/), open the top-right user icon > **User Settings > User Settings > API**, and copy your personal API key into the Dex secret `api_key`. Copy the pod-specific v3.1 API URL into `baseUrl` if it differs from the displayed na1 default. See the [connector README](../README.md) and [runnable example](examples/contact_discovery/README.md). Live provider authorization remains unverified.
