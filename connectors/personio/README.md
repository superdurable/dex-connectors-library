# Personio connector

The Go connector v0.2.0 provides `getPerson` and `listPersons` Queries. `listPersons` accepts a cursor, a page limit up to 50, exact first and last names, and ACTIVE or INACTIVE status. It returns only IDs and names plus the next cursor. The Python and TypeScript v0.2.0 packages provide both queries. A missing ID selects `notFound` for `getPerson`. Both queries retry HTTP 429 and temporary service failures.

## Connection

In Personio, open **Marketplace → Connected integrations → Create custom integration**, grant Persons read access (`personio:persons:read`), and copy the Client ID and Client Secret from the integration's Credentials page. In Dex Web **Connections**, create a `personio` connection with those values. Leave `access_token`, `baseUrl`, `timeout`, and `maxResponseBytes` blank. Dex exchanges and renews the token through Personio's client credentials endpoint. No browser redirect URI is needed. Person create and update require `personio:persons:write`, which this connection does not request; adding that scope would break existing connections.

The [runnable Go](go/examples/person-inspection/README.md), [Python](python/examples/person_inspection/README.md), and [TypeScript](typescript/examples/person-inspection/README.md) Flows use this static connection name. Optional branches are unwired and fail the Flow when selected. Real Personio authorization and retrieval have not been verified live.

The operations follow Personio's [Retrieve a person](https://developer.personio.de/reference/get_v2-persons-id), [List persons](https://developer.personio.de/reference/get_v2-persons), [Obtain Access Token](https://developer.personio.de/reference/post_v2-auth-token), and [Getting started](https://developer.personio.de/docs/getting-started-with-the-personio-api) contracts.

## Persons directory

`listPersons` reads one page with optional first name, last name, status, limit and cursor filters. Each person contains only the ID and names. Use the returned `nextCursor` in the next request, and stop when it is empty. The [Python example](python/examples/person_directory/README.md) and [TypeScript example](typescript/examples/person-directory/README.md) run the query through the `personio` connection.

## Java and Rust

Both packages expose `getPerson` and `listPersons`. Each query resolves credentials through the bound provider. A project-backed connection exchanges the integration credentials for a token with the Persons read scope and reuses it until renewal is due.

The [Java Worker](java/examples/io/superdurable/dexconnectors/personio/PersonDirectoryWorker.java) registers both Flows. Run `./gradlew personDirectoryWorker` from `java/` with `PERSONIO_CLIENT_ID` and `PERSONIO_CLIENT_SECRET` supplied by the Worker's environment. The example stores the connection and its token in memory for that process. A hosted application can pass its loaded Dex project to `PersonioClient.fromProject`.

The [Rust lookup Worker](rust/examples/get_person.rs) and [Rust directory Worker](rust/examples/list_persons.rs) load the `personio` connection from Dex project configuration. For local Dex Web, set `DEX_CONNECTOR_CONFIG_FILE` to the connections file. From `rust/`, run `cargo run --example get_person` or `cargo run --example list_persons`.

All three Workers read `DEX_FLOW_SERVICE_ADDRESS` and default to `127.0.0.1:8801`. Start the registered `JavaPersonioGetPerson`, `JavaPersonioListPersons`, `RustPersonioGetPerson`, or `RustPersonioListPersons` Flow with its query input. Use `{"personId":"3003"}` for a lookup or `{"limit":10,"status":"ACTIVE"}` for a directory page.

To generate the FDG 2.0 files from this connector directory:

```sh
for file in java/examples/io/superdurable/dexconnectors/personio/GetPersonFlow.java java/examples/io/superdurable/dexconnectors/personio/ListPersonsFlow.java rust/examples/get_person.rs rust/examples/list_persons.rs; do
  dexcli visualize "$file" --schema-version 2.0 --lint app --json --open=false --out "/tmp/personio-$(basename "$file")"
done
```
