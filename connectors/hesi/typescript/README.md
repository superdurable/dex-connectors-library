# Hesi TypeScript connector

`@superdurable/connector-hesi` implements the read-only `listDepartments` Step. It exchanges the private AppKey and AppSecurity for a short-lived access token, then reads one bounded department page. It returns only department IDs, names, codes, active flags, total, and next offset; the access token never enters Flow state.

Use the [authorization instructions](../README.md) to configure the static `hesi` connection in Dex Web. The [runnable Flow and Worker](examples/department-list/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
