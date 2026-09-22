# Dex-native Connector contract v1alpha1

The Go types in `sdk/go` are the executable form of this contract.

## Operation boundary

A Connector exposes typed `Query[IN, OUT]` and `Mutation[IN, OUT]`
operations. Applications invoke them only through `RunQuery` or `RunMutation`
from a concrete Dex Step `Execute` method.

One Step execution invokes one Connector operation. A second invocation in the
same Step would deliberately reuse the same CallID and is prohibited. Model
the second call as another Step; use `GoTo` for another execution in a loop.

Provider Query is not a Dex RPC. RPC has no Step execution ID and therefore no
provider retry or recovery boundary. Connector calls reject an empty Flow ID
or Step execution ID before resolving credentials.

## Stable CallID compatibility contract

The SDK derives CallID as UUIDv5/SHA-1 using:

1. namespace `UUIDv5(URL namespace, "https://superdurable.dev/dex-connectors/call-id/v1")`;
2. name bytes formed by these UTF-8 fields in order:
   `FlowID`, `StepExecutionID`, `ConnectorID`, `OperationID`;
3. each field prefixed by its unsigned 32-bit big-endian byte length.

Run ID, attempt, Worker identity, current time, and randomness are excluded.
Retries, Worker restart, Flow retry, and time travel into the same Step
execution therefore retain the CallID. A new Step execution gets a new ID.
Changing this derivation is a compatibility-breaking change.

## Mutation outcomes

`MutationResult` has exactly three outcomes:

| Outcome | Meaning | Process policy |
| --- | --- | --- |
| `SUCCEEDED` | Provider confirmed the write | continue or complete |
| `FAILED` | Provider confirmed rejection, auth failure, or permission failure | explicit failure branch |
| `UNKNOWN` | Request may have executed but its response was not confirmed | explicit reconciliation Query |

`FAILED` and `UNKNOWN` are values, not Dex Step errors. This prevents the Dex
retry policy from blindly repeating a provider write. Only a Connector error
classified as rate limiting or safely retryable availability can be converted
with `DexRetry`. Local validation, encoding, definition, and programming
defects remain ordinary errors.

Reconciliation is visible Process behavior. The application defines its
recovery Step and uses a Query operation to check the original CallID,
provider object ID, or provider request ID. The SDK does not auto-reconcile.

## Receipts and credentials

Receipts contain only safe provider correlation metadata. Applications should
persist a mutation receipt in an Attribute in the same Dex commit as the next
transition.

Flow input and durable state contain `ConnectionRef`, never a secret or OAuth
token. `CredentialProvider.Resolve(Call)` may authorize against Flow identity,
Step execution identity, connector and operation identity, logical connection,
and CallID. Credential values cannot be JSON, text, or YAML serialized and are
redacted by normal Go formatting. Errors and receipts never include them.

## Error taxonomy

| Kind | Meaning | Default Process policy |
| --- | --- | --- |
| `VALIDATION` | Invalid local input | fail without retry |
| `AUTHENTICATION` | Missing, expired, or revoked credential | failed result for Mutation |
| `AUTHORIZATION` | Missing scope or provider permission | failed result for Mutation |
| `NOT_FOUND` | Provider object absent | domain decision or failed Mutation |
| `CONFLICT` | Provider state conflicts | reconcile or failed Mutation |
| `RATE_LIMIT` | Provider confirmed throttling | `DexRetry` after provider hint |
| `RETRYABLE_AVAILABILITY` | Operation is confirmed safe to retry | `DexRetry` with fallback |
| `TERMINAL_REJECTION` | Provider rejected the request | failed Mutation or Query error |
| `UNKNOWN_MUTATION` | Mutation may have happened | `UNKNOWN` result and reconciliation |
| `LOCAL_DEFECT` | Connector/provider contract defect | surface and repair |

## Terminology

Connector Mutation means a provider write. Dex Action means a permissioned RPC
whose availability can depend on Flow state. The two terms are not aliases.
Hosted credential resolution and provider preview belong to the SuperVerse
Connector Service.

`connectors.dex.dev/v1alpha1` may evolve incompatibly before v1. Connector
package versions remain independent from Dex Server, dexcli, and language SDK
versions.
