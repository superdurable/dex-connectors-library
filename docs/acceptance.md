# v0.1.0-alpha.1 acceptance

## Automated checks

```bash
make check
go test -race ./...
go vet ./...
go run ./cmd/connectorctl validate connectors/http/connector.yaml connectors/openai/connector.yaml
go run ./cmd/connectorctl catalog connectors
```

Expected results:

- every legal manifest prints `valid <name> <version>`;
- malformed or unknown manifest fields fail validation;
- catalog output is deterministic and contains HTTP and OpenAI;
- CallID is stable across attempt, run, and Worker changes and changes when
  Flow, Step execution, connector, or operation identity changes;
- RPC contexts are rejected before a CredentialProvider or provider is called;
- HTTP tests cover Query, idempotent Mutation, rate limiting, unknown outcome,
  safe headers, webhook verification, and replay rejection;
- OpenAI tests cover structured output, response/request IDs, token usage,
  rate-limit metadata, confirmed failure, and unknown response recovery;
- credentials fail JSON serialization and redact `%v` and `%#v` formatting;
- React tests cover connection and reconnect states without credential props.

## Real Dex Server 0.11.1

Install Temporal CLI 1.9.1, then start Dex Server 0.11.1 while the Go SDK
remains pinned to v0.10.2:

```bash
dexcli dev
make test-integration
```

The integration suite verifies:

- a Query retries after availability failure and then succeeds;
- a rate-limited Mutation retry retains one CallID and executes once;
- a lost Mutation response returns `UNKNOWN` and enters its recovery Step;
- the receipt Attribute is visible when the recovery transition runs, proving
  that the receipt write and transition committed together;
- a Flow continues after its Worker stops and restarts at the same target;
- a real Flow RPC is rejected before it can contact the provider.

## Secret inspection

Search test output for the fixtures `super-secret`, `test-key`, and
`webhook-secret`. They must not appear in connector errors or normal logs.

## Documentation

Architecture changes update `docs/architecture.md`; contract changes update
`docs/connector-contract.md`; provider behavior belongs next to its manifest.

## UI/UX

Render each `ConnectionState` in Storybook or the consuming Studio and verify
that only normalized status/detail values reach browser props. This alpha does
not provide credential-entry forms or OAuth callback pages.
