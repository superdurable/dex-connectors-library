# Keap connector

The Go, Python, and TypeScript `getContact` Query reads one Keap CRM v2 contact by ID. It sends a read-only `GET` request for `id`, `given_name`, and `family_name` to the [Keap CRM v2 API](https://developer.keap.com/docs/api/keap). The returned `ContactProfile` contains the ID and names; names may be blank when the contact has none. HTTP 404 selects the optional `notFound` branch. Rate limits, server failures, and transport failures retry.

## Connection and configuration

For your own account, open Keap **API Settings > Personal Access Tokens**, generate a Personal Access Token, and copy its complete value into the secret `personalAccessToken` field in Dex Web Connections. The [Keap getting-started guide](https://developer.keap.com/docs/getting-started) confirms that a Personal Access Token is for calling the API as yourself; third-party apps acting for other users require OAuth 2.0. This connector's connection method does not perform OAuth. Blank token is invalid. Treat the token as a password and rotate it in Keap when needed.

Leave `baseUrl` blank for the production CRM root. Override it only for a trusted test gateway; do not append `/rest/v2/contacts`. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. In Keap CRM, open **Contacts**, select the contact, and copy its ID. Pass it as `contactId`; blank is invalid. The official [Keap v2 Postman collection](https://developer.keap.com/postman/keap.postman_collection.json) documents the retrieve-contact path, selected fields, and response names.

Runnable examples are in [`go/examples/contact-read`](go/examples/contact-read), [`python/examples/contact_read`](python/examples/contact_read), and [`typescript/examples/contact-read`](typescript/examples/contact-read). Provider conformance scenarios are recorded from Go and replayed by Python and TypeScript. Live Keap authorization and provider behavior require a user-controlled token and remain unverified here.
