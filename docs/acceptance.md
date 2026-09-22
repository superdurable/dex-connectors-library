# v0.1.0-alpha.1 acceptance

## Automated checks

```bash
make check
go run ./cmd/connectorctl validate connectors/http/connector.yaml connectors/openai/connector.yaml
go run ./cmd/connectorctl catalog connectors
```

Expected results:

- every legal manifest prints `valid <name> <version>`;
- malformed or unknown manifest fields fail validation;
- catalog output is deterministic and contains HTTP and OpenAI;
- HTTP tests cover Query, idempotent Action, rate limiting, unknown transport
  outcome, safe headers, webhook verification, and replay rejection;
- OpenAI tests cover structured output, response/request IDs, all token usage
  fields, rate-limit metadata, and retrieve-by-response-ID recovery;
- React tests cover connection and reconnect states without credential props.

## Real Dex Flow

Use Dex Server 0.11.1 and the independently published Go SDK 0.10.2:

```bash
dexcli dev
make test-integration
```

The test starts a Worker and mock provider, runs a profile Query and credit
Action, waits for Flow completion, verifies the result receipt, and confirms
the provider observed one idempotent mutation.

## Secret inspection

Search test output for the fixtures `super-secret`, `test-key`, and
`webhook-secret`. They must not appear in returned connector errors or normal
logs. Credential formatting tests enforce redaction for `%v` and `%#v`.

## Documentation

Architecture changes must update `docs/architecture.md`, contract changes must
update `runtime-contract/README.md`, and provider-specific behavior belongs
next to its connector manifest.

## UI/UX

Render each `ConnectionState` in Storybook or the consuming Studio and verify
that only normalized status/detail values reach browser props. This alpha does
not provide credential-entry forms or OAuth callback pages.
