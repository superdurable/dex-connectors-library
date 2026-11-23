# Rust Dex Connector SDK core

`dex-connectors-sdk` is the Rust core for Dex-native provider connectors. Generated
`dex-connector-<slug>` crates build on it. It supplies operation contracts, Connector Step
factories, credentials, OAuth token exchange, the single-dispatch Fence, project
configuration, Triggers, and scripted mocks. Its wire values match `sdkgo`, `sdkpython`, and
`sdkts`. Where the shared files under [`conformance/`](../conformance) pin bytes, the match is
byte for byte.

It requires Rust 1.97 or newer and `dex-sdk` 1.45.7 or newer.

## Synchronous by design

The Dex Rust SDK runs Step and RPC handlers synchronously, on a blocking thread pool, so this
crate is synchronous too. A provider operation does blocking I/O directly inside Execute, and
the Step method timeout bounds it. `ureq` works inside a Worker-executed Step. The real Dex
integration test proves this.

## Core values, failures, and retry policy

`CallId` is a UUID that stays stable across retries. `derive_call_id` builds it from the Flow
ID, the Step execution ID, the connector ID, the operation ID, and an occurrence: a SHA-1
UUIDv5 over length-prefixed fields, the same algorithm sdkgo uses.

`QueryResult`, `MutationResult`, `Receipt`, `Failure`, `QueryDefinition`,
`MutationDefinition`, and `RetryPolicy` serialize to the same camel-case JSON as sdkgo:
- An absent failure is omitted.
- Durations are integer nanoseconds.
- Timestamps follow Go RFC 3339 Nano rules (`Rfc3339NanoTimestamp`).

`FailureKind` states a fact. The operation decides whether that fact selects a branch or a
retry.

The default Execute retry policy (`default_connector_step_execute_retry_policy`):
- starts at one second,
- doubles up to five minutes,
- has no attempt limit,
- ends after one hour. When it runs out, the Flow fails.

Dex accepts only whole-second retry intervals. `RetryError::retry_after_whole_seconds`
truncates a fractional delay.

## Operations and Connector Steps

A provider operation implements `Query` or `Mutation`:
- `definition()` returns the operation's identity, branches, and Step defaults.
- `invoke(&Call, &Input)` returns a `QueryAttempt` or `MutationAttempt`.
- A Mutation also returns a stable `idempotency_key`. An empty key falls back to the Call ID.

`run_query` and `run_mutation` check the attempt against the declared branches and complete
the receipt. They return `OperationOutcome::Retry` for a retry that is safe to repeat.

The following outcomes select `defect`:
- an unknown branch,
- a receipt that belongs to another call,
- an invalid attempt,
- a second concurrent call of the same Mutation in one Step attempt.

Only a declared `uncertain` branch may represent a write that might have been applied, cannot
be repeated, and cannot be confirmed.

`QueryStep` and `MutationStep` implement `dex_sdk::Step`. Each one runs one operation per
Execute attempt.
- Each instance has a stable `step_type`, so one Flow can register several instances of the
  same generic type.
- `BranchTarget::to(&step)` wires a branch to a Step whose input is the result.
- The happy-path branch is required. An optional branch with no target fails the Flow with a
  bounded, secret-safe reason when it is selected.
- When a result Attribute is configured, the Step writes it before routing.
- `dex_retry_error` maps a `RetryError` to `HandlerError::retry_after`. A Step that calls
  `run_query` or `run_mutation` itself uses the same mapping.
- `StepAttempt` holds one Execute attempt's Call occurrences and its Fence checkpoint. It also
  holds the progress Stream and the text Stream.

The complete example below is the minimal consuming application. Its full runnable form is
`tests/dex_integration.rs`:

```rust,no_run
use std::collections::BTreeMap;
use std::sync::LazyLock;
use std::time::Duration;

use dex_connectors_sdk::dex_sdk::{
    Attribute, Context, Flow, HandlerResult, PersistenceSchema, Registry, Step, StepDecision,
    StepList,
};
use dex_connectors_sdk::{
    BranchDefinition, BranchTarget, Call, ConnectionRef, ExecuteDurability, Failure,
    FailureKind, OperationRef, Query, QueryAttempt, QueryDefinition, QueryResult, QueryStep,
    QueryStepConfig, Receipt, StepAnnotations, StepDefaults,
};

type ItemResult = QueryResult<Option<String>>;

static ITEM_RESULT: LazyLock<Attribute<ItemResult>> =
    LazyLock::new(|| Attribute::new("itemResult"));

/// Reads one item from the provider.
struct GetItem {
    endpoint: String,
}

impl Query for GetItem {
    type Input = String;
    type Output = String;

    fn definition(&self) -> QueryDefinition {
        QueryDefinition {
            operation: OperationRef::new("sample", "getItem"),
            branches: Some(vec![
                BranchDefinition::new("found", "Item exists", false),
                BranchDefinition::new("defect", "Local defect", true),
            ]),
            step_defaults: StepDefaults {
                execute_method_timeout: Duration::from_secs(10),
                execute_durability: ExecuteDurability::Async,
                ..StepDefaults::default()
            },
        }
    }

    fn invoke(&self, _call: &Call<'_>, input: &String) -> QueryAttempt<String> {
        match ureq::get(&format!("{}/items/{input}", self.endpoint)).call() {
            Ok(response) => QueryAttempt::branch(
                "found",
                response.into_string().unwrap_or_default(),
                Receipt::default(),
                None,
            ),
            Err(_) => QueryAttempt::retry(
                Failure::new(FailureKind::Availability, "sample", "getItem", "unavailable"),
                0.0,
            ),
        }
    }
}

/// Completes the Flow with the found item.
struct ItemFound;

impl Step for ItemFound {
    type Input = ItemResult;

    fn execute(&self, _context: &mut Context, input: ItemResult) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(input))
    }
}

struct ItemLookupFlow {
    get_item: QueryStep<String, GetItem>,
    found: ItemFound,
}

impl ItemLookupFlow {
    fn new(endpoint: String) -> Self {
        let found = ItemFound;
        let mut config = QueryStepConfig::new(
            "GetItem",
            StepAnnotations::new("sample", "Sample", "Get one sample item."),
            GetItem { endpoint },
            ConnectionRef::new("sample", "main"),
            |item_id: &String| item_id.clone(),
            BTreeMap::from([("found".to_string(), BranchTarget::to(&found))]),
        );
        config.result_attribute = Some(&*ITEM_RESULT);
        Self {
            get_item: QueryStep::new(config).expect("valid Connector Step"),
            found,
        }
    }
}

impl Flow for ItemLookupFlow {
    type StartInput = String;

    fn steps(&self) -> StepList<'_, String> {
        StepList::start(&self.get_item).and(&self.found)
    }

    fn persistence(&self) -> PersistenceSchema {
        PersistenceSchema::new().attribute(&ITEM_RESULT)
    }
}

let registry = Registry::new().register(ItemLookupFlow::new("https://api.example.test".into()));
assert!(registry.is_ok());
```

## Single dispatch

`singledispatch::Fence` guards one provider send that is not idempotent, for one Call and
attempt:
1. `decide()` reads this call's entry in the previous attempt's Dex heartbeat checkpoint.
2. `dispatch(send)` records the claim, calls `send` exactly once, and repeats the claim
   heartbeat every five seconds while the send runs.
3. `record_can_resend` and `record_provider_id` record what read-back proved.

Pass `Call::max_sends` as the Fence's cap. A `limited` Mutation Step sets it to the manifest's
`maxSends`, or to the Step's `max_sends_override`.

The checkpoint holds one entry per Call ID in the Step attempt, so several Fences in one Step
keep each other's entries. These checkpoint values match `single-dispatch-markers.json` byte
for byte.

Python cancels a send that is still running at the write deadline. A synchronous send cannot be
cancelled, so here `send` receives the deadline and must bound its own I/O by it.

## Credentials

`SecretString` renders as `[REDACTED]` and has no `Serialize` implementation.

`CredentialProvider` resolves a Call's credential at call time. Providers that support refresh
override `resolve_with_refresh` and `resolve_after_rejection`.

`CredentialRefreshDriver` makes the refresh decision and performs the provider exchange.
`resolve_credential` and `resolve_credential_after_rejection` coordinate the two.
`StaticCredentialProvider` serves examples and tests.

## Mocks

`connectormock` has two parts:
- Scripted `Query` and `Mutation` mocks that implement the real operation contracts.
- The Go-compatible call report file.

`ReplayStepAttempt` runs mocks and Fences outside a Worker, for scenario replay.

## Provider HTTP and OAuth

`providerhttp::ProviderHttpClient` sends provider requests under an `AddressPolicy`:
- It does not follow redirects.
- It reads response bodies up to a limit.
- It classifies errors that prove the request was never sent.
- `send_traced_before(deadline)` ends the request by a Fence write deadline.

`oauthtoken::TokenEndpoint` performs refresh-token, client-credentials, and JWT bearer token
exchanges. `ManifestCredentialRefreshDriver` implements `CredentialRefreshDriver` from a
manifest's declared token grants.

## Project configuration

`projectconfig::load_from_environment` loads one of two sources:
- the local Dex Web `connections.json` store, or
- a hosted configuration snapshot read through an `ObjectStore`.

`LoadedProject` resolves credentials, coordinating refreshes through compare-and-swap on
storage, and routes shared connections through the credential broker. Local Dex Web refreshes
are written back to `connections.json`. `projectconfig::trigger_inbox` holds the durable
Trigger inbox.

## Triggers

`trigger` provides:
- `TriggerSource`, `new_trigger`, and `deliver_trigger`. A failed delivery retries after 250 ms,
  doubling up to 30 s.
- Undeliverable classification.
- `TriggerCancellation`.
- `new_dex_flow_trigger_target`, which starts one Flow per event with the event ID as the
  request ID.

`webhooktrigger::WebhookEndpoint` handles one complete HTTP request inside the host's own
server. Requests relayed through Dex are verified with `relaysignature`.

## Differences from Python and TypeScript

- The API is synchronous.
- There is no RPC Trigger target. `dex-sdk` 1.45.7 `RpcInvokeOptions` carries no request ID,
  so the SDK cannot deliver one RPC per event.
- There is no text generation (`textgen`) module.
- There is no built-in S3 client. Hosted configuration takes an application-supplied
  `projectconfig::S3Client` through `load_from_environment_values`.
- The polling feed toolkit is not ported. Its fetcher, poller, runner, snapshot, and poll-cursor
  store are missing. The `pollingtrigger` module provides `new_trigger_credential_call` and
  `PollSchedule`.

## Testing

```sh
cargo test
dexcli dev -dex-port 19901 -web-port 19902 -open=false
DEX_SERVER_ADDRESS=127.0.0.1:19901 cargo test --test dex_integration -- --ignored --test-threads 1
```
