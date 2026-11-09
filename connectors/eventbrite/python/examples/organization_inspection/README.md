# Eventbrite Python organization inspection

This runnable Flow calls `listOrganizations` using the static `eventbrite` connection, stores the page in an Attribute, and completes with the typed result. Create an Eventbrite Private Token through Account Settings > Developer Links > API Keys, then configure the secret in Dex Web Connections. Leave the API root blank for the displayed default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/eventbrite/python`:

```bash
uv run python -m examples.organization_inspection.worker
```

Start Flow type `EventbriteOrganizationInspection` with a unique Flow ID and input `{"page":1}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.
