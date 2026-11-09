# Wrike Python connector

Implements `getTask`, returning the task ID, title, and status without description, custom fields, or assignees. Go-recorded conformance checks exact requests, branches, retries, and outputs.

Create a Permanent Access Token under **profile > Apps & Integrations > API** and save it as `api_token` in Dex Web Connections. Leave `baseUrl` blank for the US host or set `https://app-eu.wrike.com/api/v4` for an EU account. See the [connection guide](../README.md) and [runnable example](examples/task_inspection/README.md). Live provider authorization remains unverified.
