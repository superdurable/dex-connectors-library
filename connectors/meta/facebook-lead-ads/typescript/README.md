# Facebook Lead Ads TypeScript connector

Read a Page of form IDs, names, and statuses with `listLeadForms`, or one lead's ID, form ID, ad ID, and creation time with `getLead`. The connector never requests lead answers. The static `facebook-lead-ads` connection uses a Page access token; see the [connector README](../README.md) for Meta setup and the [runnable Flow](examples/lead-forms/README.md) for Dex usage. The package replays the Go-recorded provider scenarios.
