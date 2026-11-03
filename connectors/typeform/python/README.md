# Typeform Python connector

Use the generated `new_list_forms_step`, `new_get_form_step`, `new_list_responses_step`, and `new_upsert_webhook_step` factories with a static Typeform connection name. The provider client reads the account's Responses Data Center from `Config` and resolves the personal access token for each call. The optional `webhook_secret` is needed for `upsertWebhook`; it is not returned in results or receipts.

See `examples/form_catalog/` for a runnable Dex Flow and Worker. The Python package does not yet implement the `responseSubmitted` Trigger. Typeform live provider authorization and behavior have not been verified for this port.
