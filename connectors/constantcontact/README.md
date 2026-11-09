# Constant Contact connector

The Go `getContact` Query reads one contact's UUID, optional email address, names, and email permission from the [Constant Contact V3 API](https://developer.constantcontact.com/api_guide/contacts/contacts-get-one). It omits other profile fields. This initial release does not create or update contacts or receive contact events.

Create a V3 application in [My Applications](https://app.constantcontact.com/pages/dma/portal/), choose Authorization Code Flow with rotating refresh tokens, register the exact Dex Web redirect URI, and generate the Client Secret. The connection requests `contact_data` for contact reads and `offline_access` for refresh. New applications can initially be authorized only by their creator until Constant Contact approves wider access. [Constant Contact's setup guide](https://developer.constantcontact.com/api_guide/getting-started/getting-started) describes the application settings.

Run the [contact inspection example](go/examples/contact-inspection/README.md) with the static `constantcontact` connection. The input `contactId` is a V3 contact UUID. Transport failures, HTTP 429, request timeouts, and server failures retry. A definitive 404 selects `notFound`; other provider rejections and malformed or oversized responses select optional branches. Unwired optional branches fail the Flow.

The Go, Python, and TypeScript packages implement `getContact` and replay the Go-recorded scenarios. The proposed contact mutation and event source remain future work. OAuth authorization and a real Constant Contact request have not been verified against a live account.
