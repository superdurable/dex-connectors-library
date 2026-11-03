# PagerDuty connector for TypeScript

Install `@superdurable/connector-pagerduty@0.1.0`. The package implements the manifest's `listIncidents` and `getIncident` Queries, with no Triggers. It requires `@superdurable/connectors-sdk@0.1.0` and `@superdurable/dex>=1.28.0`.

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listIncidents` | `newListIncidentsStep` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getIncident` | `newGetIncidentStep` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listIncidents` requests a page of 1–100 incidents; zero limit selects 25. A negative offset is a local defect. `getIncident` escapes the supplied ID as a URL path segment. HTTP 408, 429, 5xx, and transport errors retry. All other non-200 responses are terminal provider rejections, except HTTP 404 on `getIncident`, which selects `notFound`.

Create a Dex Web connection named `pagerduty` using the [authorization instructions](../README.md#authorization). The API key is resolved for each attempt and sent only in the `Authorization` header. Leave configuration fields blank for the manifest defaults.

The [incident review example](examples/incident-review/README.md) runs a real Dex Worker and persists a safe incident page or detail. Run `npm run check` for unit, conformance, type, and lint checks. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration` against `dexcli dev` to test the example through a real Worker and Client.

Live PagerDuty authorization and provider responses remain unverified; use a user-controlled Dex Web connection to exercise them.
