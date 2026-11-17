# Keap contact lookup Flow

This runnable Worker registers `KeapContactRead`, `KeapContactDirectory`, `KeapCompanyLookup`, `KeapContactNoteLookup`, and `KeapContactTags` through the static `keap` connection. They read one contact, list one page of contacts, read one company, read one contact note, and list one page of applied tags, respectively. Configure secret `personalAccessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/keap/go`, run:

```bash
dexcli visualize ./examples/contact-read/flow/directory_workflow.go --schema-version 2.0 --lint app --json --out /tmp/keap-directory-fdg
dexcli visualize ./examples/contact-read/flow/company_workflow.go --schema-version 2.0 --lint app --json --out /tmp/keap-company-fdg
dexcli visualize ./examples/contact-read/flow/note_workflow.go --schema-version 2.0 --lint app --json --out /tmp/keap-note-fdg
dexcli visualize ./examples/contact-read/flow/tag_workflow.go --schema-version 2.0 --lint app --json --out /tmp/keap-tag-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override keap=<artifact-directory>`. In Dex Web Connections select the `keap` connection. Follow the [connection guide](../../../README.md) to enter secret `personalAccessToken`; leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/keap/go`:

```bash
GOWORK=off go run ./examples/contact-read
```

Start `KeapContactRead` with a unique Flow ID and input `{"contactId":"123"}`. Replace the sample ID with the ID of a contact in your Keap CRM. Start `KeapContactDirectory` with `{"pageSize":20}`; pass a returned `nextPageToken` as `pageToken` to continue. Start `KeapCompanyLookup` with `{"companyId":"321"}` using an ID from Keap CRM > Companies. Start `KeapContactNoteLookup` with `{"contactId":"123","noteId":"456"}` using IDs from Keap CRM > Contacts > Notes. Start `KeapContactTags` with `{"contactId":"123","pageSize":20}` and use `nextPageToken` for later pages. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Keap authorization and provider behavior remain unverified.
