# Eventbrite connector

The Go connector reads accessible organization IDs with `listOrganizations`, lists an organization's events with `listEvents`, and retrieves one attendee's ID, event ID, status, and creation time with `getAttendee`. It excludes attendee profiles, answers, barcode values, and email from Flow results.

In [Eventbrite Account Settings > Developer Links > API Keys](https://www.eventbrite.com/account-settings/apps), create an API key if necessary and copy its **Private Token** into the Dex Web secret field. The provider receives it only in an `Authorization: Bearer` header. The default API root is `https://www.eventbriteapi.com/v3`; the endpoint override exists for trusted test gateways.

Run [organization-inspection](go/examples/organization-inspection/README.md) with the static `eventbrite` connection to obtain an organization ID. Pass that ID to `listEvents`; use an event and attendee ID for `getAttendee`. The page input is one-based, with zero selecting page one. Both lists return provider pagination metadata.

Queries retry transport failures, HTTP 429, request timeout, and server errors. A definitive 404 selects `notFound` when applicable. Rejections and malformed or oversized responses select optional branches. Unwired optional branches fail the Flow. The [Python package](python/README.md) implements all three Queries and replays the Go-verified conformance scenarios. TypeScript and independent language package releases remain pending.
