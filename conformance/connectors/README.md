# Connector operation conformance

Each `conformance/connectors/<directory>/scenarios.json` replays provider responses through the Go connector and its Python and TypeScript ports. The porter authors scenario identity (`name`, `operation`, `callId`), `input`, optional `attempts`, `priorHeartbeat`, and `fixedClock`, and ordered provider responses (`status`, `headers`, `body`) or `transportFailure` (`before-send` or `after-send`). Use inert placeholder credentials. Keep provider data realistic and nonempty where the successful operation returns data.


`priorHeartbeat` restores a previous attempt's persisted value. The SDK Fence marker keeps its flat `{ "callId", "attempt", "state" }` shape. For a connector-owned heartbeat, use `{ "attempt": 1, "value": <exact heartbeat JSON> }`; `value` is opaque to the shared harness and should come from a Go-recorded heartbeat. Go record mode writes the last heartbeat stored by every attempt as `intermediate[].heartbeat` or `expected.heartbeat` for the final attempt. `{ "cleared": true }` records Go `RecordHeartbeat(nil)` and requires Python `heartbeat()` or TypeScript `recordHeartbeat(undefined)`; a present JSON `null` remains `heartbeat: null`. An absent `heartbeat` key means Go made no heartbeat write on that attempt, even if it restored a prior value. The Python and TypeScript runners require the port to make a write exactly when Go did and compare its SDK-encoded JSON with the Go value. After an intermediate attempt, they restore the Go value for the next attempt.

If a heartbeat includes dispatch time, set `fixedClock` to an RFC 3339 UTC timestamp ending in `Z`, with at most three fractional second digits. Go adapters pass `conformanceFixedClock(t, scenario)` to the connector clock option. Python client factories read `recorder.scenario["fixedClock"]`, and TypeScript adapters read `scenario.fixedClock`; runner contexts expose the same instant as `fixed_clock` and `fixedClock`. `flow_started_at` and `flowStartedAt` remain Unix second 1, matching Go's context. This keeps the persisted timestamp and read-back query deterministic across languages.

From the connector's `go/` directory, run `CONNECTOR_CONFORMANCE_MODE=record GOWORK=off go test . -run '^TestConnectorConformance$' -count=1`. The generated Go reference test replaces all derived expectations in `scenarios.json`: each request's method and path, every query and form value, full JSON body or decoded Gmail MIME, connector-sent headers, the production idempotency key, intermediate retries, final branch and failure kind, retry delay, uncertainty, canonical Go-marshaled output, and provider request count. In outputs only, a nil Go slice or map emitted as a JSON field is recorded as `[]` or `{}` at any nesting depth, matching generated Python and TypeScript empty collection defaults. Byte slices (including `json.RawMessage`) and types implementing `json.Marshaler` retain their exact JSON representation. Fields omitted by `omitempty` or `omitzero` stay omitted; all other values compare exactly. Commit the resulting fixture. A normal Go test records again in memory and fails if any committed expectation differs. Do not hand-edit derived expectations.

Every scenario must match the Go recording; `pending` is rejected. The union of inputs for each operation must set every Go `Input` field to a nonzero value at least once, including optional fields. Every non-retry branch has an exact `output`; an empty or null output is still recorded. Each operation needs separate 429 and 5xx Retry scenarios whose classified status is the first exchange of the final attempt, a transport-failure scenario, each declared branch, and uncertainty when declared. Intermediate decisions record cumulative `exchangeCount`, which identifies the start of the final attempt. Cover additional Go-tested duplicate-safety and read-back behavior.

The Python and TypeScript runners compare requests and production Dex SDK codec outputs by exact equality. SDK field defaults determine omission; the runners do not re-encode values by hand. They invoke mutations through the production SDK path, which derives the idempotency key from the operation and Call ID; they never inject the fixture key. Gmail `requestBody.$rawMime` contains the full decoded MIME message, including subject, recipients, and body. The runners ignore transport-added `Host`, `Connection`, `Content-Length`, and `Accept-Encoding` headers, plus unmodified HTTP client defaults for `Accept` and `User-Agent`. `requestCount` includes requests that reached the provider, including read-backs and after-send failures; before-send failures consume an attempt but do not count as provider requests.

Request header names match without case sensitivity, while their values compare exactly. Go recordings retain the connector's header spelling, including non-canonical names such as `orgId`; port runners must accept that spelling in fixtures.

For HTTP `multipart/form-data`, `multipart/mixed`, and `multipart/related`, Go records `Content-Type` without its random `boundary` parameter but retains every other media type parameter. `requestBody.$multipart` is an ordered list of parts; each part has exact `headers` values (including `Content-Disposition` name and filename and `Content-Type`) and `bodyBase64` for the exact bytes, including binary content. Part headers use UTF-8 values, canonical header names, unfolded lines, and trimmed surrounding whitespace. All three runners parse each request using its actual boundary, compare every part and its order, and ignore only the outer boundary value. Other request bodies and headers still compare exactly. Gmail's decoded `$rawMime` is separate from this HTTP multipart format.

For connector-generated random JSON request values, add a scenario `randomSeed` beginning `SYNTHETIC_CONFORMANCE_` and `randomRequestFields` containing simple JSON pointers such as `/passwordProfile/password`. The Go conformance adapter injects `newConformanceSyntheticRandomReader(scenario.RandomSeed)` through the connector's test-only construction seam. Python and TypeScript adapters use `SyntheticRandomSource` from their shared runners. Each stream block is SHA-256 of the UTF-8 seed followed by an eight-byte big-endian counter, starting at zero. For bounded choices, Python `go_random_int` and TypeScript `goRandomInt` reproduce Go `crypto/rand.Int` byte consumption and rejection sampling. The connector's normal client still uses `crypto/rand.Reader`.

Go record mode replaces each declared random string in the request body with `{"$syntheticSha256":"..."}`. Every runner hashes the actual field before comparing the complete JSON body, so a changed character fails while no password appears in the committed fixture or mismatch output. Each declared pointer must occur in at least one request, and a random field requires a synthetic seed. Do not put a generated password, token, or nonce in scenario input, output, or provider response.

Run `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/<directory>` locally. `connectorctl generate --check` checks the generated Go test. CI runs all fixture-backed changed connectors and `python3 script/connector_conformance.py --mutation-self-test`, which corrupts representative branch, output (including a wrong nonempty list), request, MIME, idempotency, read 5xx, intermediate and final heartbeat shapes, extra heartbeats, and pending fields to prove the runners reject them. Multipart part body/presence/order mutations run when a connector fixture contains a multipart request with at least two parts. Part-name mutations need `Content-Disposition`; multipart/related also mutates a part `Content-Type`. New or edited Python and TypeScript packages require fixtures. Existing packages without fixtures warn `missing conformance (legacy)` on Go-only changes. Provider replay supplements the connector's runnable Dex example; it is not live-provider verification.

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
