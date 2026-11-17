# Go identity lookup, creation, and email send Flows

The `AmazonSESIdentityLookup` Flow lists one page of identities, then calls `getEmailIdentity` through the static `amazon-ses` connection. It stores the selected `EmailIdentityDetails` in an Attribute and exposes it through read-only Dex Web RPCs. `AmazonSESSimpleEmailSend` sends one plain text message; `AmazonSESTemplatedEmailSend` sends a stored template with JSON variables. Both complete with the SES message ID. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amazon/ses/go`, run:

```bash
dexcli visualize ./examples/identity-lookup/flow/workflow.go --schema-version 2.0 --json --out /tmp/amazon-ses-identity-lookup-fdg
dexcli visualize ./examples/identity-lookup/flow/send_email_flow.go --schema-version 2.0 --json --out /tmp/amazon-ses-email-send-fdg
dexcli visualize ./examples/identity-lookup/flow/create_email_identity_flow.go --schema-version 2.0 --lint app --json --out /tmp/amazon-ses-identity-create-fdg
dexcli visualize ./examples/identity-lookup/flow/send_templated_email_flow.go --schema-version 2.0 --lint app --json --out /tmp/amazon-ses-templated-send-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override amazon-ses=<artifact-directory>`. In Dex Web Connections select the `amazon-ses` connection. Set `region` and secret `access_key_id` and `secret_access_key` as described in the [connector README](../../../README.md). Set `session_token` for temporary credentials. Leave `endpoint`, `timeout`, and `maxResponseBytes` blank for the documented behavior and displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/amazon/ses/go`:

```bash
GOWORK=off go run ./examples/identity-lookup
```

Start `AmazonSESIdentityLookup` with a unique Flow ID and input `{"emailIdentity":"example.com"}`. The Worker connects to Dex Server without an inbound listener.

To send, start `AmazonSESSimpleEmailSend` with a unique Flow ID and input `{"fromEmailAddress":"sender@example.com","toEmailAddress":"reader@example.net","subject":"Report ready","textBody":"The report is ready."}`. The sender must be verified in the configured Region; sandbox accounts also restrict recipients.

To send a stored template, first create it under [SES Console > Email templates](https://console.aws.amazon.com/ses) in the same Region, or call `createEmailTemplate` with a name and subject plus text or HTML content. Start `AmazonSESTemplatedEmailSend` with a unique Flow ID and input `{"fromEmailAddress":"sender@example.com","toEmailAddress":"reader@example.net","templateName":"weekly-report","templateData":{"name":"Ada"}}`. The template variables must form a JSON object.

To start verification, start `AmazonSESCreateEmailIdentity` with a unique Flow ID and input `{"emailIdentity":"example.com"}`. For a domain, publish the returned DKIM tokens as CNAME records according to SES Console. An email address receives a verification link. The mock-backed real-Dex test starts all four Flows.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
