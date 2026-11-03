# Connector operation conformance

Each `conformance/connectors/<directory>/scenarios.json` replays provider responses through the Go connector and its Python and TypeScript ports. The porter authors scenario identity (`name`, `operation`, `callId`), `input`, optional `attempts`, `priorHeartbeat`, and `clock`, and ordered provider responses (`status`, `headers`, `body`) or `transportFailure` (`before-send` or `after-send`). Use inert placeholder credentials. Keep provider data realistic and nonempty where the successful operation returns data.

Set `clock` to an RFC 3339 UTC timestamp ending in `Z`, with whole seconds or one to three fractional digits, such as `"2026-10-26T12:00:00.123Z"`, when an output, retry delay, or request depends on wall time. Calendar dates and hours must be valid. The generated Go recorder exposes `conformanceClock(t, scenario)`; Python and TypeScript runners expose `scenario_clock(scenario)` and `scenarioClock(scenario)`. The clock takes effect only after each connector's conformance adapter passes the returned function to its clock dependency. Omitting `clock` keeps the connector's real-time default. The fixed value is reused for every attempt in that scenario.

From the connector's `go/` directory, run `CONNECTOR_CONFORMANCE_MODE=record GOWORK=off go test . -run '^TestConnectorConformance$' -count=1`. The generated Go reference test replaces all derived expectations in `scenarios.json`: each request's method and path, every query and form value, full JSON body or decoded Gmail MIME, connector-sent headers, the production idempotency key, intermediate retries, final branch and failure kind, retry delay, uncertainty, canonical Go-marshaled output, and provider request count. In outputs only, a nil Go slice or map emitted as a JSON field is recorded as `[]` or `{}` at any nesting depth, matching generated Python and TypeScript empty collection defaults. Byte slices (including `json.RawMessage`) and types implementing `json.Marshaler` retain their exact JSON representation. Fields omitted by `omitempty` or `omitzero` stay omitted; all other values compare exactly. Commit the resulting fixture. A normal Go test records again in memory and fails if any committed expectation differs. Do not hand-edit derived expectations.

Every scenario must match the Go recording; `pending` is rejected. The union of inputs for each operation must set every Go `Input` field to a nonzero value at least once, including optional fields. Every non-retry branch has an exact `output`; an empty or null output is still recorded. Each operation needs separate 429 and 5xx Retry scenarios whose classified status is the first exchange of the final attempt, a transport-failure scenario, each declared branch, and uncertainty when declared. Intermediate decisions record cumulative `exchangeCount`, which identifies the start of the final attempt. Cover additional Go-tested duplicate-safety and read-back behavior.

The Python and TypeScript runners compare requests and production Dex SDK codec outputs by exact equality. SDK field defaults determine omission; the runners do not re-encode values by hand. They invoke mutations through the production SDK path, which derives the idempotency key from the operation and Call ID; they never inject the fixture key. Gmail `requestBody.$rawMime` contains the full decoded MIME message, including subject, recipients, and body. The runners ignore transport-added `Host`, `Connection`, `Content-Length`, and `Accept-Encoding` headers, plus unmodified HTTP client defaults for `Accept` and `User-Agent`. `requestCount` includes requests that reached the provider, including read-backs and after-send failures; before-send failures consume an attempt but do not count as provider requests.

For HTTP `multipart/form-data` and `multipart/mixed`, Go records `Content-Type` without its random `boundary` parameter but retains every other media type parameter. `requestBody.$multipart` is an ordered list of parts; each part has exact `headers` values (including `Content-Disposition` name and filename and `Content-Type`) and `bodyBase64` for the exact bytes, including binary content. Part headers use UTF-8 values, canonical header names, unfolded lines, and trimmed surrounding whitespace. All three runners parse each request using its actual boundary, compare every part and its order, and ignore only the outer boundary value. Other request bodies and headers still compare exactly. Gmail's decoded `$rawMime` is separate from this HTTP multipart format.

Run `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/<directory>` locally. `connectorctl generate --check` checks the generated Go test. CI runs all fixture-backed changed connectors and `python3 script/connector_conformance.py --mutation-self-test`, which corrupts representative branch, output (including a wrong nonempty list), request, MIME, idempotency, read 5xx, and pending fields to prove the runners reject them. Multipart part name/body/presence/order mutations run when a connector fixture contains a multipart request with at least two parts. New or edited Python and TypeScript packages require fixtures. Existing packages without fixtures warn `missing conformance (legacy)` on Go-only changes. Provider replay supplements the connector's runnable Dex example; it is not live-provider verification.

## Native wire protocol exemptions

The reviewed list in [`conformance/exemptions.yaml`](../exemptions.yaml) exempts
PostgreSQL, MySQL, SQL Server, and Email. Their native database, SMTP, and IMAP
protocols cannot be recorded by the HTTP replay runner. The conformance gate
reports `exempt (reason)` and rejects exemptions for HTTP connectors. Changing
the list requires a tooling PR.

Each exempt Python or TypeScript package needs unit tests for every Go-tested
branch, Retry, uncertain decision, and protocol-error classification. It also
needs real protocol integration tests in CI service containers. Database ports
follow the Go connector's live-test scenarios and connection variables when
available. Email ports mirror the Go connector's in-process `internal/mailtest`
tests, including `send_message_test.go` and `search_messages_test.go`.
The GreenMail CI job exports `EMAIL_CONNECTOR_TEST_*` variables for SMTPS,
IMAPS, and a test mailbox, with a CA certificate that verifies localhost and
127.0.0.1. Its plaintext SMTP and IMAP ports cover refusal to authenticate
without TLS. GreenMail has no STARTTLS, so protocol unit fakes cover SMTP and
IMAP STARTTLS behavior in each language port.
Place Python integration tests in `python/tests/integration/` and TypeScript
integration tests in `typescript/test/integration.test.ts`. Developers do not
run Docker locally. CI selects only exempt connectors with a language package.
