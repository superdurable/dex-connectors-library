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

- manifests reject `idempotency: provider`, Mutation `idempotency: none`,
  unknown progress capabilities, duplicates, and unknown fields;
- catalog output is deterministic and reports OpenAI `structured` and `text`;
- operation interfaces compile only with strict Attempt returns;
- zero/invalid Query Attempt becomes `LOCAL_DEFECT/FAILED` and zero/invalid
  Mutation Attempt becomes `LOCAL_DEFECT/UNKNOWN`;
- Failure and Unknown return nil Go error; explicit Retry is the only non-nil
  connector error path;
- a Retry delay becomes `dex.RetryAfter`; no delay uses the Step's policy;
- the same Failure kind can be either Failure or Retry;
- Call ID remains stable across attempt, Run, and Worker changes and changes
  with Flow, Step execution, connector, or operation identity;
- provider idempotency derivation is stable, empty derivation falls back to
  Call ID, and Mutation Call/Receipt contain the final key;
- RPC invocation fails before credential or provider access;
- HTTP tests cover auth, not-found, rejection, Query retry, safe Mutation
  retry, post-dispatch Unknown, provider-specific key derivation, response
  bounds, safe headers, webhook signature, and replay rejection;
- OpenAI tests cover non-streaming usage, SSE created/deltas/completed/failed/
  incomplete/error, unknown events, early EOF, bounds, and reconciliation;
- credential values, API keys, authorization headers, and provider bodies do
  not enter Failure, Receipt, Stream, or test output;
- React tests remain green with no credential values in browser props.

## Real Dex Server 0.11.1

Install Temporal CLI 1.9.1, then start Dex Server 0.11.1 while the Go SDK
remains pinned to v0.10.2:

```bash
dexcli dev
make test-integration
```

The integration suite verifies:

- a Query Retry is executed by the real Dex retry policy and then succeeds;
- a Query Failure does not retry and enters an explicit failure path;
- a rate-limited Mutation retry retains one Call ID and idempotency key and
  produces one provider write;
- a lost Mutation response returns Unknown and enters recovery without
  repeating the write;
- the receipt Attribute is readable in recovery, proving the receipt write and
  transition committed together;
- a Flow continues reconciliation after its Worker restarts;
- a real Flow RPC cannot contact a provider;
- OpenAI mock SSE writes structured and buffered text Streams through a real
  Worker, preserves text order, and flushes the final chunk;
- progress messages contain the same Call ID and retries are distinguishable by
  Attempt and Sequence.

## Manual provider checks

1. Run `connectorctl catalog` twice and diff the output.
2. Confirm OpenAI Create reports `idempotency: required` and progress
   capabilities `structured`, `text`.
3. Configure a provider-specific HTTP key derivation and verify the same key is
   sent for every retry and copied to the receipt.
4. Disconnect a Mutation after accepting its body and verify the Process enters
   reconciliation rather than repeating the Mutation.
5. Interrupt an OpenAI SSE stream after `response.created`; verify Unknown
   retains the response ID for RetrieveResponse.

## Secret inspection

Search captured test output for `super-secret`, `test-key`,
`credential store unavailable`, authorization values, and mock provider error
bodies. None may appear in Failure, Receipt, Stream messages, or normal logs.

## Documentation

Contract changes update `docs/connector-contract.md`; component and data-flow
changes update `docs/architecture.md`; provider behavior is documented beside
its manifest and in this acceptance guide.

## UI/UX

No React or Studio behavior changes in G2a. Existing React tests and build must
remain green. A later Studio uses Streams only for live feedback and Flow
snapshot/results for authoritative status.
