# Typeform Python connector

Use the generated `new_list_forms_step`, `new_get_form_step`, `new_list_responses_step`, and `new_upsert_webhook_step` factories with a static Typeform connection name. The provider client reads the account's Responses Data Center from `Config` and resolves the personal access token for each call. The optional `webhook_secret` is needed for `upsertWebhook`; it is not returned in results or receipts.

See `examples/form_catalog/` for the operation Flow and [response intake](examples/response_intake/README.md) for a signed `responseSubmitted` Trigger Flow and Worker. The Trigger verifies the raw Typeform signature, decodes typed answers with field definitions, filters by form ID, and records accepted events in a durable inbox before acknowledgment. Live Typeform authorization and delivery have not been verified for this port.

Rate limit retries honor Typeform's integer or HTTP-date `Retry-After` header up to one hour. `upsertWebhook` returns the `defect` branch for malformed webhook URLs, including non-printable characters.
