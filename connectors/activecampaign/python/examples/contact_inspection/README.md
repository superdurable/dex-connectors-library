# ActiveCampaign Python contact inspection

This runnable Flow calls `listContacts` using the static `activecampaign` connection, stores the page in an Attribute, and completes with the typed result. Sign in at https://www.activecampaign.com/login, open Settings > Developer, copy the account API URL ending in `/api/3` into `baseUrl`, and save the user-specific API Key in the secret field. Blank `baseUrl` is invalid; blank timeout and response-size fields use their displayed defaults.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/activecampaign/python`:

```bash
uv run python -m examples.contact_inspection.worker
```

Start Flow type `ActiveCampaignContactInspection` with a unique Flow ID and input `{"offset":0,"limit":20}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.
