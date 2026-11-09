# Facebook Lead Ads Python connector

Read Page lead-form IDs, names, and statuses with `listLeadForms`, or read one lead's ID, form ID, ad ID, and creation time with `getLead`. The connector never requests form questions or lead answers.

The package uses the static `facebook-lead-ads` connection and a Page access token. See the [connector README](../README.md) for Meta setup and the [runnable Flow](examples/lead_forms/README.md) for Dex usage. Go-recorded provider scenarios are replayed by `tests/test_conformance.py`.
