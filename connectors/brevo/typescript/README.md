# Brevo TypeScript connector

Install `@superdurable/connector-brevo@0.1.1` for Dex TypeScript applications.
The package implements `createOrUpdateContact` and `getContact`, and exports
typed Step factories, connection helpers, generated models, and manifest mocks.
Both operations accept plain RFC 5322 addresses using the same parsed-address check as the Go connector.

The [contact sync example](examples/contact-sync) is the runnable application.
It binds a Dex Web connection named `brevo-contacts`, writes a contact by email,
reads the contact back, and returns its ID and email. The API key is resolved
privately for each call. Configure it in [Brevo API keys](https://app.brevo.com/settings/keys/api)
under **Settings > SMTP & API > API keys**; do not put it in Flow input.

`createOrUpdateContact` retries transport failures, 408, 409, 429, and 5xx.
Brevo's `updateEnabled` option makes the write repeatable. A 201 response returns
its new ID; a 204 update returns the email and `created: false`. `getContact`
returns `notFound` for 404. Malformed or oversized responses choose
`invalidResponse`; local validation chooses `defect`. Other provider rejections
choose `providerRejected`. Optional branches are unwired by default and fail
the Flow when selected.

Run `npm ci`, `npm run check`, and the example's real Dex integration test.
Go-recorded provider scenarios are in
[`conformance/connectors/brevo/scenarios.json`](../../../conformance/connectors/brevo/scenarios.json).
Live Brevo authorization and real-provider calls have not been verified.
