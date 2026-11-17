# Keap connector

The Go, Python, and TypeScript `getContact` Query reads one Keap CRM v2 contact by ID. Go also provides `listContacts`, `upsertContact`, and `deleteContact`. These use the [Keap CRM v2 contact API](https://developer.keap.com/docs/api/keap). The returned profiles contain only IDs and names. HTTP 404 selects the optional `notFound` branch on ID lookup and deletion. Rate limits, server failures, and transport failures retry.

`listContacts` takes a page size of 1–100 and an opaque continuation token; zero size uses 20 and blank token starts the first page. `upsertContact` uses Keap's `duplicate_option=Email` to update the matching email or create a contact, so repeating the same request is safe. Provide a valid email (at most 75 characters) and bounded names. `deleteContact` removes the selected ID. The Go operations use safe repeat semantics.

## Connection and configuration

For your own account, open Keap **API Settings > Personal Access Tokens**, generate a Personal Access Token, and copy its complete value into the secret `personalAccessToken` field in Dex Web Connections. The [Keap getting-started guide](https://developer.keap.com/docs/getting-started) confirms that a Personal Access Token is for calling the API as yourself; third-party apps acting for other users require OAuth 2.0. This connector's connection method does not perform OAuth. Blank token is invalid. Treat the token as a password and rotate it in Keap when needed.

Leave `baseUrl` blank for the production CRM root. Override it only for a trusted test gateway; do not append `/rest/v2/contacts`. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. In Keap CRM, open **Contacts**, select the contact, and copy its ID. Pass it as `contactId`; blank is invalid. For upsert, use that contact's email or a new email you intend to add. The official [Keap v2 contact API](https://developer.keap.com/docs/api/keap) documents the list, upsert, and deletion paths.

Runnable examples are in [`go/examples/contact-read`](go/examples/contact-read), [`python/examples/contact_read`](python/examples/contact_read), and [`typescript/examples/contact-read`](typescript/examples/contact-read). The Go example also registers a contact-directory Flow. Provider conformance scenarios are recorded from Go; Python and TypeScript replay only their published `getContact` capability. Live Keap authorization and provider behavior require a user-controlled token and remain unverified here.
