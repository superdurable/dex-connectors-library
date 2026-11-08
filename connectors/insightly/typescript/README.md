# Insightly TypeScript connector

This package implements `listContacts` and `getContact` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Results contain contact IDs, names, and email addresses; custom fields and links remain outside Flow results.

Sign in to [Insightly CRM](https://login.insightly.com/), open the top-right user icon > **User Settings > User Settings > API**, and copy your personal API key into Dex secret `api_key`. Copy the pod-specific v3.1 API URL into `baseUrl` if it differs from the displayed na1 default. See the [connector README](../README.md) and [contact discovery example](examples/contact-discovery/README.md).

JavaScript numbers cannot represent provider numeric IDs above the safe integer limit exactly; such responses select `invalidResponse`. Live Insightly authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
