# DocuSign envelope-signing example

This example sends one envelope from a template and waits, durably and for as
long as signers take, for its outcome. One `EnvelopeSigningFlow` runs per
signature request:

1. `RecordSigningRequest` validates the request and stores it in the
   `docusign-signing-state` Attribute.
2. `SendEnvelope` runs `createEnvelopeFromTemplate`, tagging the envelope with
   the custom field `dexSigningRequestId`. `providerRejected`, `uncertain`,
   and `defect` go to `RecordSendFailure`, which fails the Flow; an uncertain
   create is never repeated automatically.
3. `RecordEnvelopeSent` stores the envelope ID and the signing deadline.
4. `AwaitEnvelopeOutcome` waits for the first of a Connect event on the
   `docusign-envelope-events` Channel or the next status poll time. The
   `envelopeEventReceived` Trigger delivers each verified event to the
   `ReceiveEnvelopeEvent` RPC, which ignores a repeated event ID and queues
   the event only while the Flow waits for this envelope.
5. Without a Connect event, `CheckEnvelopeStatus` (`getEnvelope`) runs at the
   poll time and `RecordEnvelopeStatus` routes a final status, waits again, or
   at the deadline goes to `VoidExpiredEnvelope` (`voidEnvelope`).
6. A completed envelope goes to `DownloadSignedDocument`
   (`downloadCombinedDocument`), which writes the combined PDF with its
   certificate of completion to `SIGNED_DOCUMENT_DIR`, and
   `RecordSignedDocument` completes the Flow with the PDF's size and SHA-256.
   A declined or voided envelope completes as `declined` or `voided` through
   `RecordEnvelopeClosed`; one voided by the deadline completes as `expired`.
   `RecordVoidRefusal` downloads an envelope that completed just as the
   deadline passed and closes one that was declined.

Every Step that changes the state locks the Attribute, so a Connect event
cannot interleave with a transition; an event that meets the held lock is
retried by the Trigger delivery until the Step commits. The Flow output and `GetSigningState`
return the same record.

## 1. Generate the Flow Definition

From `connectors/docusign`, generate strict FDG 2.0 with the latest stable
dexcli release
(`python3 script/dex_compatibility.py install-dexcli --output <path>` installs
it):

```bash
mkdir -p build
dexcli visualize ./examples/envelope-signing/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/envelope-signing
```

The command must report `valid: true` with 14 Steps, the wait on the
`docusign-envelope-events` Channel and its Timer, every connector branch
routed, and the `envelope-outcomes` binding. Inside this repository it also
warns `connector_release_required` and `connector_trigger_release_required`,
because a local module is not a published release.

## 2. Start Dex with this connector's release metadata

From the repository root, build release metadata from this source; the
connector has no Studio bundle:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/docusign-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/docusign/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/docusign \
  --version v0.21.0 --tag connectors/docusign/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/docusign-release/connector-release.json \
  --digest-output /tmp/docusign-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/docusign/build" \
  --connector-release-override docusign=/tmp/docusign-release
```

## 3. Configure the connection

Open the Dex Web URL that dexcli prints, select **Connectors**, and select
**DocuSign eSignature** (connection `docusign-esignature`). Choose
**Developer account** for a free developer account at
`https://developers.docusign.com`, or **Production account**, and follow the
guide shown above the form:

- `oauth_client_id` and `oauth_client_secret`: the app's Integration Key and a
  secret key from **Settings > Integrations > Apps and Keys**, after adding
  the Redirect URI that Dex Web shows;
- `connect_hmac_key`: a key from **Connect > Connect Keys**;
- `accountId`: blank to use the authorizing user's default account.

Choose **Connect**, grant consent as the user who sends envelopes, and save.
Then save the Flow's `envelope-outcomes` binding. The connector declares no
Studio units, so Dex Web saves the binding as `{}`, which records all three
outcomes. To record fewer, set `events` in the binding's record in the
`triggerBindings` array of the project configuration the application loads:

```json
{
  "connectorId": "docusign",
  "connectionName": "docusign-esignature",
  "triggerName": "envelopeEventReceived",
  "bindingName": "envelope-outcomes",
  "configuration": {"events": ["envelope-completed", "envelope-declined", "envelope-voided"]}
}
```

## 4. Create the Connect configuration

Expose `http://127.0.0.1:8862/docusign/connect` through an HTTPS tunnel and
create a Custom Connect configuration with that URL as described in the
[connector README](../../README.md#connect-configuration): JSON SIM format,
the Envelope Signed/Completed, Declined, and Voided events, Include Data:
Custom Fields, Include HMAC Signature, and Require Acknowledgement.

## 5. Run the Worker and send an envelope

Run the Worker from `connectors/docusign`. It reads the `DEX_PROJECT_*` project
configuration environment described in
[project configuration](../../../../sdkgo/projectconfig/README.md); Dex Web or
Superverse Studio writes that configuration when you save the connection and
the binding:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/docusign"
go run ./examples/envelope-signing
```

The Worker listens on `127.0.0.1:8861` and the Connect endpoint on
`127.0.0.1:8862`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `CONNECT_BIND_ADDRESS`, `DEX_BLOB_CACHE_DIR`,
`SIGNED_DOCUMENT_DIR`, `SIGNING_STATUS_POLL_INTERVAL` (default six hours), or
`SIGNING_DEADLINE` (default 14 days) when needed. `GET /readyz` answers `200`
once the binding receives deliveries.

In Dex Web **Start Flow**, start `EnvelopeSigningFlow` with the Flow ID
`docusign-signing-opp-123`, which must be `docusign-signing-` plus the request
ID, and a template whose role is `Customer`:

```json
{
  "requestId": "opp-123",
  "templateId": "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a",
  "signers": [{"roleName": "Customer", "name": "Priya Raman", "email": "priya@meridian.example.com"}],
  "emailSubject": "Meridian Corp: Master Services Agreement"
}
```

The run waits in `AwaitEnvelopeOutcome` with status `waitingForSignature`.
Sign the envelope from the email. Connect delivers `envelope-completed`, the
Flow resumes at once, and the completed run's output looks like this:

```json
{"request": {"requestId": "opp-123", "templateId": "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a", "signers": [{"roleName": "Customer", "name": "Priya Raman", "email": "priya@meridian.example.com"}], "emailSubject": "Meridian Corp: Master Services Agreement"}, "status": "completed", "envelopeId": "93be49ab-0000-0000-0000-000000000001", "sentAt": "2026-10-04T12:00:00Z", "deadline": "2026-10-18T12:00:00Z", "envelopeStatus": "completed", "receivedEventIds": ["93be49ab-0000-0000-0000-000000000001:envelope-completed"], "resumedBy": "connect", "document": {"envelopeId": "93be49ab-0000-0000-0000-000000000001", "contentType": "application/pdf", "byteCount": 182344, "sha256": "4f1c9a1d0e5b5f0b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b", "includesCertificate": true, "storedLocation": "00000000-0000-0000-0000-000000000000-93be49ab-0000-0000-0000-000000000001-with-certificate.pdf"}}
```

## 6. Deliver, redeliver, and forge

To test the endpoint without signing, send a Connect message signed the way
DocuSign signs it, the base64 HMAC-SHA256 of the body, with the envelope ID
from the run:

```bash
envelope=93be49ab-0000-0000-0000-000000000001
body='{"event":"envelope-declined","apiVersion":"v2.1","retryCount":0,"configurationId":1,"generatedDateTime":"2026-10-04T13:00:00.0000000Z","data":{"accountId":"00000000-0000-0000-0000-000000000000","envelopeId":"ENVELOPE","envelopeSummary":{"status":"declined","customFields":{"textCustomFields":[{"name":"dexSigningRequestId","value":"opp-123"}]}}}}'
body=${body/ENVELOPE/$envelope}
signature=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$DOCUSIGN_CONNECT_HMAC_KEY" -binary | base64)
curl -i http://127.0.0.1:8862/docusign/connect \
  -H 'Content-Type: application/json' \
  -H "X-DocuSign-Signature-1: $signature" \
  --data-binary "$body"
```

- The endpoint answers `200` and the Flow completes as `declined`.
- Sending it again answers `200`; the RPC reports a duplicate, or the closed
  Flow consumes it, and nothing changes.
- Changing the body after signing, or signing with another key, answers `400`
  and records nothing.
- A message without `dexSigningRequestId` answers `200`; `AcceptEnvelopeEvent`
  consumes it without calling the Flow.

## Test

From `connectors/docusign`:

```bash
go test -race ./examples/envelope-signing/...
```

The unit tests cover the Flow's mapping and admission rules and the document
store, and serve the example's Trigger target against an unreachable Dex
Server: a signed delivery is answered `200` and retried toward Dex, a wrongly
signed one `400`, and an ignored event `200` without a record.

The real Dex tests run the Flow on a Worker and serve its Trigger target,
without the durable project inbox, against `dexcli dev` and a TLS stand-in for
`account.docusign.com` and `na3.docusign.net`: a forged and an uncorrelated
delivery leave the Flow waiting; a signed `envelope-completed` resumes it and
its redeliveries download nothing again; a Worker replaced during the wait
resumes the same Flow from a Connect decline; a silent Connect falls back to
the status poll; and the signing deadline voids the envelope:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/envelope-signing/... -count=1 -v
```
