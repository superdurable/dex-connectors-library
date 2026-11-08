# ActiveCampaign connector

The Go connector lists contact IDs, email addresses, and names with `listContacts` and retrieves one contact with `getContact`. Embedded relationships, custom field values, activity, deals, and provider links are excluded from Flow results. The account's API Key is sent in the `Api-Token` header, never in a URL or result.

Sign in to [ActiveCampaign](https://www.activecampaign.com/login), then open Settings > Developer. Copy the full account-specific API URL into `baseUrl` and the current user's API Key into the secret `api_key` field. The API URL ends in `/api/3`; blank `baseUrl` prevents connection creation. The [contact-inspection Flow](go/examples/contact-inspection/README.md) is the runnable consumer. See the [authentication guide](https://developers.activecampaign.com/reference/authentication) and [contact API](https://developers.activecampaign.com/reference/list-all-contacts).

Both reads retry transport failures, HTTP 429, request timeout, and server failures. `getContact` selects `notFound` on a definitive 404. Provider rejections and malformed or oversized responses select optional branches. An unwired optional branch fails the Flow. The initial release provides read operations; write and event capabilities require separate delivery and duplicate-safety contracts.
