# Connector library architecture

## Repository boundaries

- `schema/` owns the portable manifest schema and strict loader.
- `sdk/go/` owns runtime identities, credential resolution, receipts, and
  typed errors.
- `sdk/react/` owns credential-free connection status UI primitives.
- `connectors/` owns provider adapters and one manifest per adapter.
- `cmd/connectorctl/` validates manifests and emits a deterministic catalog.
- `runtime-contract/` documents invariants shared by every language binding.
- `test/mock-provider/` provides deterministic provider behavior.
- `examples/customer-onboarding/` proves the contract in a real Dex Flow.

## Query and Action lifecycle

```text
Dex Step              Connector                 Provider
   |                      |                         |
   | ConnectionRef        | resolve credential      |
   |--------------------->|------------------------>|
   | Query                | bounded read            |
   |--------------------->|------------------------>|
   | create/persist CallID|                         |
   | Action(CallID)       | idempotency key         |
   |--------------------->|------------------------>|
   |                      |<-- result or timeout ---|
   | receipt / UNKNOWN    |                         |
   | recover(CallID)      | query-before-mutate     |
```

External effects run only in Dex `Execute`. A `WaitFor` implementation must
not resolve credentials or call a provider. The example creates the Action
identity in the Query Step, persists it in the next Step input and an
Attribute, then uses a separate recovery Step for unknown mutation outcomes.

## HTTP safety

The generic HTTP connector allows `GET`/`HEAD` Queries and
`POST`/`PUT`/`PATCH`/`DELETE` Actions. Targets must remain inside an explicit
host allowlist, non-loopback transport must use HTTPS, response bodies are
bounded, secret request headers can only be populated from
`CredentialProvider`, and response headers are reduced to a safe metadata
allowlist before they can enter Flow state.

Webhook verification covers an HMAC-SHA256 signature over timestamp and body,
a bounded timestamp window, and caller-supplied replay storage.

## OpenAI safety and accounting

The OpenAI connector calls the Responses API, maps structured output to
`text.format`, and returns response ID, model, request ID, rate-limit headers,
and all available usage dimensions: input, cached input, output, reasoning
output, and total tokens. A known response ID can be retrieved for recovery;
without one, a transport failure remains an unknown mutation.

## UI/UX

The React package normalizes `not_configured`, `connecting`, `connected`,
`expired`, `revoked`, `insufficient_scope`, and `error`. It exposes Connect or
Reconnect actions without accepting any credential-value prop.
