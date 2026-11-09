# Google Contacts TypeScript connector

This package implements `listContacts`, `getContact`, and `createContact` through the [Google People API](https://developers.google.com/people/api/rest). Use a static `google-contacts` Dex Web connection authorized with the `contacts` OAuth scope. The [list Flow](examples/contacts/README.md) and [creation Flow](examples/creation/README.md) are runnable examples.

The list operation uses a fixed names and email field mask and returns one page. The get operation accepts a `people/{id}` resource name. Create requires `givenName` and accepts optional `familyName` and `emailAddress`. Because Google has no create-contact idempotency key, the Mutation uses a durable single-dispatch fence. An `uncertain` result requires inspecting Contacts before creating another.

Run `npm ci` and `npm run check`. Live Google OAuth and API behavior have not been verified in this checkout.
