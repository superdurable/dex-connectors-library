# Google Contacts Python connector

The Python package implements `listContacts`, `getContact`, and `createContact` from the [Google People API](https://developers.google.com/people/api/rest). It uses the static `google-contacts` Dex Web connection and the manifest OAuth scope `contacts`.

The [runnable list Flow](examples/contacts/README.md) starts with `ListContactsInput()` and persists one contact page. The [creation Flow](examples/creation/README.md) exercises `createContact`. The create operation requires `given_name`; its single-dispatch fence counts possible sends. An unknown outcome is resent until the send cap (`max_sends`, default 3), a saved resource name is read back instead of resent, and a definite refusal is never resent. If the outcome is still unknown at the cap, the operation selects `uncertain`; inspect Contacts before trying again.

Run `uv sync --group dev` and `uv run pytest`. Live Google OAuth and API behavior have not been verified in this checkout.
