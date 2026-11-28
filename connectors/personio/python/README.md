# Personio Python connector

Read a person by ID with `getPerson` or list a filtered page with `listPersons`. Go-recorded conformance checks exact requests, branches, retries, and outputs. Configure a custom integration with `personio:persons:read` using the [connection guide](../README.md). The [runnable Dex Flow](examples/person_inspection/README.md) exercises the package. Live Personio authorization remains unverified.

## Persons directory

`listPersons` reads one page with optional first name, last name, status, limit and cursor filters. Each person contains only the ID and names. Use the returned `nextCursor` in the next request, and stop when it is empty. The [Python example](examples/person_directory/README.md) exposes the next cursor as `PersonPage.next_cursor`.
