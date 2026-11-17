# Creatio contact inspection Flow

These runnable Flows call `getContact`, `listContacts`, `createContact`, `listAccounts`, and `createAccount` through the static `creatio` connection. They persist typed results in Attributes and expose them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/creatio/go`, run:

```bash
dexcli visualize ./examples/contact-inspection/flow/workflow.go --schema-version 2.0 --lint app --open=false --json --out /tmp/creatio-contact-inspection-fdg
dexcli visualize ./examples/contact-inspection/flow/list_contacts.go --schema-version 2.0 --lint app --json --out /tmp/creatio-contacts-search-fdg
dexcli visualize ./examples/contact-inspection/flow/create_contact.go --schema-version 2.0 --lint app --json --out /tmp/creatio-contact-creation-fdg
dexcli visualize ./examples/contact-inspection/flow/list_accounts.go --schema-version 2.0 --lint app --json --out /tmp/creatio-accounts-search-fdg
dexcli visualize ./examples/contact-inspection/flow/create_account.go --schema-version 2.0 --lint app --json --out /tmp/creatio-account-creation-fdg
```

Run with `--lint app --open=false` for strict FDG 2.0 validation.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override creatio=<artifact-directory>`. In Dex Web Connections, select `creatio` and follow the [connection guide](../../../README.md) to create Creatio client credentials. Enter the required application `baseUrl` and Identity Service `tokenEndpoint`; leave `access_token`, `timeout`, and `maxResponseBytes` blank.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/creatio/go`:

```bash
GOWORK=off go run ./examples/contact-inspection
```

Start `CreatioContactInspection` with a unique Flow ID and input `{"contactId":"90a68db5-cb3a-4f64-bb60-c0a934ee7600"}`. Replace the sample ID with a contact ID you can access. The Worker connects to Dex Server without an inbound listener.

Start `CreatioContactSearch` with input `{"pageNumber":1,"pageSize":25}` and a unique Flow ID. Continue paging until fewer than 25 contacts are returned.

Start `CreatioContactCreation` with input `{"name":"Ada Lovelace","email":"ada@example.com"}` and a unique Flow ID. Check for an existing contact first because an email/name match cannot prove which create request succeeded.

Start `CreatioAccountSearch` with input `{"pageNumber":1,"pageSize":25}` and a unique Flow ID. Continue paging until fewer than 25 accounts are returned.

Start `CreatioAccountCreation` with input `{"name":"Example Company"}` and a unique Flow ID. Account names are not unique, so an ambiguous earlier send can select `uncertain`.

CI runs the mock-backed real-Dex integration test. Real Creatio authorization and retrieval remain unverified.
