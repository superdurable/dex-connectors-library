# Typeform response-recorder example

This example receives signed Typeform webhooks and starts one
`TypeformResponseRecorder` Flow per submitted response:

1. the Worker mounts `NewLocalResponseSubmittedEndpointRunner` at
   `/webhooks/typeform` and starts it at once, so a submission is recorded in
   the binding's durable inbox even while Dex is unreachable;
2. the `response-submitted` binding of the `responseSubmitted` Trigger keeps
   only submissions of the form chosen with the `formPicker` unit, and
   `AcceptSubmission` admits only a submission with at least one answer;
3. `sdkgo.NewDexFlowTriggerTarget` starts the Flow with ID
   `typeform-<form id>-<response token>` and the Trigger event ID as request
   ID, so a redelivered submission starts no second Flow;
4. `RecordSubmission` stores the typed answers, each keyed by field ID and
   ref, and the hidden fields in the `typeform-submission` Attribute;
5. `ReadForm` calls `getForm` for the submitted form;
6. `RecordQuestions` pairs every question of the form, in form order, with
   its answer by field ID, stores them in the `typeform-questions` Attribute,
   and completes the Flow; a skipped question has no answer, and an answer to a
   question the form no longer has is kept in `unmatchedAnswers`.
   `RecordReadFailure` stores a `notFound`, `providerRejected`,
   `invalidResponse`, or `defect` outcome with its safe message and fails the
   Flow.

Each submission costs one `getForm` call, and Typeform allows two API requests
per second per account; a form with more submissions than that should record
the webhook's answers without reading the form.

## 1. Generate the Flow Definition

From `connectors/typeform`, generate strict FDG 2.0 with the dexcli release
pinned in the repository's `.dex-compat-version` file
(`python3 script/dex_compatibility.py install-dexcli --output <path>` installs
it):

```bash
mkdir -p build
dexcli visualize ./examples/response-recorder/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/response-recorder
```

The command must report `valid: true` with the four Steps, all five `getForm`
branches routed, and the `response-submitted` binding with its `formPicker`
unit. Inside this repository it also warns `connector_release_required` and
`connector_trigger_release_required`, because a local module is not a
published release.

## 2. Start Dex with this connector's release metadata

The connector is part of this module, so Dex needs release metadata and the
Studio bundle built from this source; without them the connection shows
**Unsupported**. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
npm ci --prefix sdk/react && npm run build --prefix sdk/react
npm ci --prefix connectors/typeform/ui && npm run build --prefix connectors/typeform/ui
mkdir -p /tmp/typeform-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/typeform/connector.yaml \
  --ui-root connectors/typeform/ui/dist \
  --output /tmp/typeform-release/connector-ui.tgz \
  --digest-output /tmp/typeform-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/typeform/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/typeform \
  --version v0.1.0 --tag connectors/typeform/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/typeform-release/connector-ui.tgz \
  --ui-digest /tmp/typeform-release/connector-ui.tgz.sha256 \
  --output /tmp/typeform-release/connector-release.json \
  --digest-output /tmp/typeform-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/typeform/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override typeform=/tmp/typeform-release
```

## 3. Configure the connection

Open the Dex Web URL that dexcli prints, select **Connectors**, and select
**Typeform** (connection `typeform-forms`). Follow the guide shown above the
form:

- `access_token`: a token from Typeform **Account > Personal tokens**
  (<https://admin.typeform.com/user/tokens>) with the scopes `forms:read`,
  `responses:read`, and `webhooks:write`;
- `webhook_secret`: the output of `openssl rand -hex 32`. Export the same value
  as `TYPEFORM_WEBHOOK_SECRET` in the terminal that registers the webhook and
  sends test deliveries;
- leave the configuration fields blank unless the account is in an EU
  Responses Data Center, and save.

Dex Web writes the credentials of the record in
`$HOME/.dex/connectors/connections.json` like this:

```json
{"auth_method": "personal-access-token", "access_token": "<personal access token>", "webhook_secret": "<openssl rand -hex 32>"}
```

Then open the Flow's `response-submitted` binding, choose **Load forms** in the
**Form** unit, select the form whose submissions start the Flow, and save. The
saved binding configuration looks like this; an empty `formId` records
submissions of every form whose webhook points at this application:

```json
{
  "connectorId": "typeform",
  "connectionName": "typeform-forms",
  "triggerName": "responseSubmitted",
  "bindingName": "response-submitted",
  "configuration": {"formId": "lT4Z3j"}
}
```

## 4. Register the webhook

Expose `http://127.0.0.1:8872/webhooks/typeform` through an HTTPS tunnel;
Typeform accepts only HTTPS webhook URLs with a certificate it can validate.
A Flow can call `upsertWebhook`, which creates or replaces the form's webhook
with one tag and registers `webhook_secret`; by hand it is the same `PUT`:

```bash
curl -s -X PUT "https://api.typeform.com/forms/lT4Z3j/webhooks/dex-response-recorder" \
  -H "Authorization: Bearer $TYPEFORM_TOKEN" -H 'Content-Type: application/json' \
  --data "{\"url\":\"https://<tunnel>/webhooks/typeform\",\"enabled\":true,\"secret\":\"$TYPEFORM_WEBHOOK_SECRET\",\"event_types\":{\"form_response\":true}}"
```

Running it again with the same tag updates that webhook instead of adding one.
In the Typeform editor the same webhook appears under **Connect > Webhooks**,
where **Edit > Secret** must hold the same `webhook_secret`.

## 5. Run the Worker and send a delivery

In a second terminal, from `connectors/typeform`:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/typeform"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/response-recorder
```

The Worker listens on `127.0.0.1:8871` and the webhook endpoint on
`127.0.0.1:8872`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `WEBHOOK_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when
needed, and set `LOG_LEVEL=debug` to see every delivery. `GET /readyz` answers
`200` once the binding receives submissions.

Submit the form from its public link. To test without a respondent, send one
signed delivery the way Typeform signs it, `sha256=` followed by the base64
HMAC-SHA256 of the raw body keyed with the secret; replace `lT4Z3j` with the
ID of a form your token can read, so the Flow's `getForm` call finds it:

```bash
body='{"event_id":"LtWXD3crgy","event_type":"form_response","form_response":{"form_id":"lT4Z3j","token":"a3a12ec67a1365927098a606107fac15","landed_at":"2026-09-30T11:50:00Z","submitted_at":"2026-09-30T11:58:59Z","hidden":{"user_id":"abc123456"},"definition":{"id":"lT4Z3j","title":"Lead intake","fields":[{"id":"SMEUb7VJz92Q","ref":"email","title":"Your email?","type":"email"}]},"answers":[{"type":"email","email":"ada@example.com","field":{"id":"SMEUb7VJz92Q","type":"email"}}]}}'
signature=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$TYPEFORM_WEBHOOK_SECRET" -binary | base64)
curl -i http://127.0.0.1:8872/webhooks/typeform \
  -H 'Content-Type: application/json' \
  -H "Typeform-Signature: sha256=$signature" \
  --data-binary "$body"
```

The endpoint answers `200`, and Dex Web shows the run
`typeform-lT4Z3j-a3a12ec67a1365927098a606107fac15`. Its `typeform-submission`
Attribute holds the answers, each with the ref the webhook's form definition
gives its field. For a form with that email question and one skipped yes/no
question, the completed run's `typeform-questions` Attribute is:

```json
{"branch": "found", "questions": [{"field": {"id": "SMEUb7VJz92Q", "ref": "email", "title": "Your email?", "type": "email", "isRequired": true}, "answer": {"fieldId": "SMEUb7VJz92Q", "fieldRef": "email", "fieldType": "email", "fieldTitle": "Your email?", "type": "email", "email": "ada@example.com"}}, {"field": {"id": "RUqkXSeXBXSd", "ref": "consent", "title": "May we follow up?", "type": "yes_no", "isRequired": false}}]}
```

## 6. Redeliver and forge

- Send the same `curl` again. It answers `200`, and no second Flow starts: the
  Flow ID is already taken, so the start is reported as a duplicate.
- Change the body after signing, for example replace `ada@` with `eve@`, or
  sign with another secret. The endpoint answers `400` and nothing is
  recorded; Typeform retries a `400` on a back-off of up to four hours and
  then gives up.
- Send `"event_type":"form_response_partial"`, or another form's `form_id`.
  It answers `200` without a record.
- Send `"answers":[]`. It answers `200`; `AcceptSubmission` consumes it
  without a Flow.

## 7. Binding not running

Until the Worker's binding receives submissions, for example while it replays
its inbox at startup, the endpoint answers `503`. Typeform retries a `503`
every two to three minutes for up to ten hours, and the retry that arrives once
the binding runs is answered `200` and starts the Flow.

## 8. Restart recovery

Stop Dex, send a delivery, and stop the Worker. The endpoint answered `200`
because the delivery was on disk; it stays in the binding's inbox, a
`.trigger-inbox-*.json` file beside the connection file. Start Dex and the
Worker again: the runner replays it, logs `replaying pending trigger events`,
and the Flow starts and completes.

## Test

From `connectors/typeform`:

```bash
go test -race ./examples/response-recorder/...
```

The unit tests cover the Flow's mapping and admission rules, and run the
Worker against an unreachable Dex Server: a signed delivery is answered `200`
only after it is in the inbox, a tampered or wrongly signed one `400`, and a
delivery for another form or a partial response `200` without a record.

The real Dex tests run the example against `dexcli dev` and a TLS stand-in for
`api.typeform.com`: a signed delivery starts exactly one Flow that records its
typed answers and reads the form once; a redelivery starts no second Flow and
reads nothing again; a forged delivery answers `400` and starts nothing; a
submission without answers is filtered; a delivery while the binding is not
running answers `503` and its retry starts the Flow; and a delivery
acknowledged while Dex was unreachable is replayed after a restart:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/response-recorder/... -count=1 -v
```
