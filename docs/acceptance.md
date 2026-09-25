# Connector modules v0.1.0 acceptance

## Automated checks

```bash
cd sdkgo
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

cd connectors/github
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

cd connectors/google/spreadsheet
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ui
npm ci
npm test
npm run build
cd ../../../..

cd connectors/google/gmail
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ui
npm ci
npm test
npm run build
cd ../../../..

cd connectors/linkedin
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../openai
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

make check
go test -race ./...
go vet ./...
go run ./cmd/connectorctl validate connectors/github/connector.yaml connectors/google/gmail/connector.yaml connectors/google/spreadsheet/connector.yaml connectors/linkedin/connector.yaml connectors/openai/connector.yaml connectors/slack/connector.yaml
go run ./cmd/connectorctl generate --check connectors/github/connector.yaml
go run ./cmd/connectorctl generate --check connectors/google/gmail/connector.yaml
go run ./cmd/connectorctl generate --check connectors/google/spreadsheet/connector.yaml
go run ./cmd/connectorctl generate --check connectors/linkedin/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
go run ./cmd/connectorctl release-workflow --check connectors .github/workflows/release-connector.yml
go run ./cmd/connectorctl catalog connectors
```

The suite must prove:

- the SDK and every provider connector compile and test independently of
  `go.work`;
- generated operation-specific factories expose typed branch fields and typed,
  non-serializable connector Connections;
- the generated release dropdown matches the catalog and release artifacts are
  deterministic, versioned, and checksummed;
- typed `sdkgo.GoTo` targets can be converted into generic branch targets
  without exposing their underlying Dex Step;
- component release planning handles first, minor, patch, major, no-change,
  path-scoped, and breaking-change cases;
- branch definitions reject empty, duplicate, invalid, and missing defect
  branches while allowing Mutations to omit uncertainty;
- factories reject missing, duplicate, and unknown targets and require every
  target to accept the typed factory output;
- Query invalid Attempt routes to defect, while Mutation invalid Attempt and
  explicit uncertainty route to uncertainty;
- Retry is the only Attempt that returns a Go error;
- configured Result Attributes and Streams are used directly by the factory;
- operation defaults merge with non-zero application Execute overrides and
  WaitFor-only options are rejected;
- Call ID and idempotency key are stable across Dex attempt, Run, and Worker
  changes and change with Step execution or operation identity;
- RPC fails before credentials and providers;
- manifest generation is deterministic, `--check` catches drift, and generated
  Config/Credentials/branches/definitions compile;
- OAuth2 and OIDC fixtures preserve protocol, connection kind, HTTPS endpoints,
  discovery metadata, nonce, scopes, PKCE, operation authorization, and typed
  secret fields without implementing provider OAuth;
- `SecretString`, API keys, OAuth tokens, authorization headers, and provider
  bodies do not enter Result, Failure, Receipt, Stream, or logs;
- OpenAI provider classification, idempotency, response bounds, SSE,
  uncertainty, and recovery tests remain green;
- GitHub profile/email selection, scope/revocation branches, response bounds,
  public-only pagination, deduplication, sorting, truncation, and rate-limit
  delay tests remain green;
- LinkedIn OIDC UserInfo claim filtering, verified-email, authorization,
  not-found, response-bound, redirect, and rate-limit tests remain green;
- Sheets query-before-mutate, duplicate-key, bounded-response, and uncertainty
  tests remain green;
- Gmail sender validation, MIME encoding, terminal rejection, rate-limit, and
  uncertain-send tests remain green;
- Studio Host API, setup component, deterministic tarball, digest, and React
  build tests remain green.

## Real Dex Server 0.11.3

Install Temporal CLI 1.9.1 and start Dex Server/dexcli 0.11.3 while the Go SDK
uses v0.11.3:

```bash
dexcli dev
make test-integration
```

The integration suite verifies:

- a standalone fixture connector consumes only the public SDK API to define
  typed Connection/Credentials, Query/Mutation operations, and
  operation-specific factories;
- the fixture connector registers those factories in a real Dex Flow and
  preserves Call ID, idempotency, Attribute, Stream, and retry behavior;
- provider connector examples register factory Steps with typed `StepRef` targets;
- GitHub and LinkedIn signup profile factories persist their typed Result
  Attributes atomically with terminal transitions;
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
- Sheets query-before-mutate commits one stable keyed row and the branch
  transition with its Result Attribute.
- Gmail unknown sends route to the uncertainty branch without an automatic
  resend.

## Manual API and Dex Web acceptance

1. Read the generated provider files and confirm YAML is the only source of
   Config, Credentials, branch constants, definitions, and defaults.
2. Confirm a Flow can be authored with factory values directly inside
   `dex.DefineStep`/`dex.DefineStartStep`, without provider concrete Steps.
3. Confirm every branch has one target and Mutation uncertainty cannot be
   mistaken for a provider rejection.
4. Confirm `ConnectionRef` is bound at Flow registration, not accepted from
   public start input.
5. Confirm the Flow explicitly registers each Attribute and Stream passed to a
   Connector Step.
6. Generate schema 2.0 with dexcli 0.11.3 and verify factory Steps, branches,
   Result Attributes, and Streams render in Dex Web 2.0.

## Release acceptance

1. Run `Release Connector` for `github` with the default minor bump and confirm
   tag `connectors/github/v0.1.0` plus its release artifact.
2. Run the opt-in GitHub live test with a dedicated account and exact
   `read:user user:email` scopes; confirm no private repository data is read.
3. Run `Release Connector` for `linkedin` with the default minor bump and
   confirm tag `connectors/linkedin/v0.1.0` plus its release artifact.
4. Run the opt-in LinkedIn live test with a dedicated member and exact
   `openid profile email` scopes; confirm only OIDC UserInfo claims are read.
5. In clean temporary modules, download each public tag with `GOWORK=off` and
   compile its operation-specific factory example.
6. Confirm each release note contains only commits that changed that connector
   and retains `## Breaking Changes` with `None.` when appropriate.
7. Run `Release Connector` for `google/spreadsheet` and confirm its independent
   tag plus `connector-release.json` and `connector-ui.tgz` digests.
8. Run `Release Connector` for `google/gmail` and confirm its own tag and UI
   artifacts contain no Sheets-only changes.

The Dex CLI analyzer shipped in 0.11.3. Connector releases must retain a
deterministic schema 2.0 golden and inspect nodes, branches, Attribute edges,
and Stream edges in Dex Web 2.0.

## Documentation and UI/UX

Contract changes update `connector-contract.md`; component/data flow changes
update `architecture.md`; provider manifests and their examples show canonical
authoring. The shared G2c contract ships the bundle format and
protocol; SuperVerse G6a owns the BFF loader, OAuth callbacks, credential
broker, and Studio environment pages.
