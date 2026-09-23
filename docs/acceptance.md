# v0.1.0-alpha.1 acceptance

## Automated checks

```bash
cd sdk/go
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

make check
go test -race ./...
go vet ./...
go run ./cmd/connectorctl validate connectors/http/connector.yaml connectors/openai/connector.yaml
go run ./cmd/connectorctl generate --check connectors/http/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
go run ./cmd/connectorctl catalog connectors
```

The suite must prove:

- the SDK compiles and tests independently of `go.work`;
- typed `connector.GoTo` targets can be converted into generic branch targets
  without exposing their underlying Dex Step;
- component release planning handles first, minor, patch, major, no-change,
  path-scoped, and breaking-change cases;
- branch definitions reject empty, duplicate, invalid, missing defect, and
  missing Mutation uncertainty branches;
- factories reject missing, duplicate, and unknown targets and require every
  target to accept the typed factory output;
- Query invalid Attempt routes to defect, while Mutation invalid Attempt and
  explicit uncertainty route to uncertainty;
- Retry is the only Attempt that returns a Go error;
- Result Attribute requirement and Stream capability/name checks fail during
  factory construction;
- operation defaults merge with non-zero application Execute overrides and
  WaitFor-only options are rejected;
- Call ID and idempotency key are stable across Dex attempt, Run, and Worker
  changes and change with Step execution or operation identity;
- RPC fails before credentials and providers;
- manifest generation is deterministic, `--check` catches drift, and generated
  Config/Credentials/branches/definitions compile;
- OAuth fixture preserves connection kind, endpoints, scopes, PKCE, and typed
  secret fields without implementing provider OAuth;
- `SecretString`, API keys, OAuth tokens, authorization headers, and provider
  bodies do not enter Result, Failure, Receipt, Stream, or logs;
- HTTP and OpenAI provider classification, idempotency, response bounds, SSE,
  and recovery tests remain green;
- React connection-state tests and build remain unchanged.

## Real Dex Server 0.11.1

Install Temporal CLI 1.9.1 and start Dex Server/dexcli 0.11.1 while the Go SDK
remains v0.10.2:

```bash
dexcli dev
make test-integration
```

The integration suite verifies:

- Customer Onboarding registers factory Steps with typed `StepRef` targets;
- Query Retry succeeds under the generated Dex retry defaults;
- terminal Query branch does not use Dex retry;
- Mutation rate-limit retry retains Call ID/idempotency key and creates one
  provider write;
- post-dispatch uncertainty routes to a reconciliation factory Query without
  repeating the Mutation;
- Result Attribute and branch transition commit together;
- a registered target's real Step options apply after StepRef resolution;
- Worker restart resumes recovery;
- Flow RPC cannot invoke a provider;
- a factory OpenAI Mutation writes structured/text Streams, preserves text
  order, flushes the tail, and persists its typed Result Attribute;
- retry Stream messages retain Call ID and separate Attempt/Sequence.

## Manual API acceptance before Dex CLI work

1. Read the generated HTTP/OpenAI files and confirm YAML is the only source of
   Config, Credentials, branch constants, definitions, and defaults.
2. Confirm a Flow can be authored with factory values directly inside
   `dex.DefineStep`/`dex.DefineStartStep`, without provider concrete Steps.
3. Confirm every branch has one target and Mutation uncertainty cannot be
   mistaken for a provider rejection.
4. Confirm `ConnectionRef` is bound at Flow registration, not accepted from
   public start input.
5. Confirm `PersistenceRequirements()` matches resources explicitly registered
   by the Flow.
6. Accept the known limitation that dexcli 0.11.1 cannot yet render factory
   Steps in Dex Web 2.0.

Only after this API acceptance should the separate Dex CLI analyzer patch
begin. After its patch release, regenerate Customer Onboarding schema 2.0,
add the deterministic golden, inspect nodes/branches/Attribute/Stream edges in
Dex Web 2.0, and then move Connector PR #1 from Draft to Ready.

## Documentation and UI/UX

Contract changes update `connector-contract.md`; component/data flow changes
update `architecture.md`; provider manifests and the Customer Onboarding README
show canonical authoring. G2a adds no React form, OAuth callback, or Studio
page. Manifest metadata is consumed by later G2c/G6a UI work.
