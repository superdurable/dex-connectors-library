# Connector library architecture

## Repository boundaries

- `schema/` owns the portable manifest schema and strict loader.
- `sdk/go/` owns Dex-native call identity, credential resolution, results,
  receipts, and typed errors.
- `sdk/react/` owns credential-free connection status UI primitives.
- `connectors/` owns provider adapters and one manifest per adapter.
- `cmd/connectorctl/` validates manifests and emits a deterministic catalog.
- `docs/connector-contract.md` defines cross-connector invariants.
- `test/mock-provider/` provides deterministic provider behavior.
- `examples/customer-onboarding/` proves the contract in a real Dex Flow.

## Dex composition

```text
ReadCustomerProfileStep
  Execute -> RunQuery(http.query)
                 |
                 v
GrantCustomerCreditsStep
  Execute -> RunMutation(http.mutation)
                 |
       +---------+---------+
       |         |         |
   SUCCEEDED   FAILED    UNKNOWN
       |         |         |
   complete   failure   ReconcileCreditGrantStep
                         Execute -> RunQuery(http.query)
```

Every provider call runs in a concrete Dex Step `Execute`. One Step execution
invokes at most one Connector operation. The application, rather than the SDK,
owns stable Step types, inputs, retry policy, receipt Attributes, recovery,
transitions, and terminal behavior.

`RunQuery` and `RunMutation` create a `Call` from the active `dex.Context`.
They reject RPC invocations because an RPC has no Step execution ID. The SDK
does not provide generic Query or Mutation Steps.

Provider Query and Dex RPC are different concepts. Provider Query performs an
external read inside a Step. Dex RPC synchronously reads or changes an existing
Flow, receives an Event, exposes a permission-checked Dex Action, or schedules
a Step. Studio provider previews use the SuperVerse Connector Service backend
API, not a Flow RPC.

## HTTP safety

The generic HTTP connector allows `GET`/`HEAD` Queries and
`POST`/`PUT`/`PATCH`/`DELETE` Mutations. Targets remain inside an explicit host
allowlist, non-loopback transport uses HTTPS, response bodies are bounded,
secret request headers come only from `CredentialProvider`, and response
headers are reduced to a safe allowlist before entering Flow state.

Webhook verification covers an HMAC-SHA256 signature over timestamp and body,
a bounded timestamp window, and caller-supplied replay storage.

## OpenAI safety and accounting

The OpenAI connector calls the Responses API, maps structured output to
`text.format`, and returns response ID, model, request ID, rate-limit headers,
and all available usage dimensions: input, cached input, output, reasoning
output, and total tokens. A known response ID can be retrieved by an explicit
recovery Step; without one, a lost create response remains `UNKNOWN`.

## UI/UX

The React package normalizes `not_configured`, `connecting`, `connected`,
`expired`, `revoked`, `insufficient_scope`, and `error`. It exposes Connect or
Reconnect controls without accepting any credential-value prop. Connector
operation kinds are `query` and `mutation`; `action` is reserved for a Dex
Action RPC.
