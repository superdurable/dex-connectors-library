# Runtime contract v1alpha1

The Go types in `sdk/go` are the executable form of this contract.

## Boundary rules

1. Flow inputs and state contain only `ConnectionRef`, never a secret or OAuth
   token. A server-side `CredentialProvider` resolves the reference during
   `Execute`.
2. A Query is read-only and may be retried after a typed rate-limit or
   availability error.
3. An Action has a stable UUID `CallID`. The consuming Flow creates and
   persists it before the provider mutation.
4. Every successful Action returns a safe `Receipt`. A transport failure after
   dispatch is `UNKNOWN_MUTATION`, not an ordinary retryable error.
5. Unknown mutations are reconciled by provider object ID, response ID, or
   idempotency key before another mutation is attempted.
6. Connector errors expose only a sanitized message. Provider response bodies,
   credential values, and secret headers do not enter errors or logs.

## Error taxonomy

| Kind | Meaning | Default Flow policy |
| --- | --- | --- |
| `VALIDATION` | Invalid local input | fail without retry |
| `AUTHENTICATION` | Missing, expired, or revoked credential | reconnect/fail |
| `AUTHORIZATION` | Missing scope or provider permission | request scope/fail |
| `NOT_FOUND` | Provider object absent | domain decision |
| `CONFLICT` | Provider state conflicts | query and reconcile |
| `RATE_LIMIT` | Provider throttled the call | retry after hint |
| `RETRYABLE_AVAILABILITY` | Read call was not completed | retry with backoff |
| `TERMINAL_REJECTION` | Provider rejected the request | fail without retry |
| `UNKNOWN_MUTATION` | Action may have happened | query receipt/status |
| `LOCAL_DEFECT` | Invalid connector/provider contract | surface and repair |

## Versioning

`connectors.dex.dev/v1alpha1` may evolve incompatibly before v1. Published
manifests state their connector package version independently from Dex Server,
dexcli, and language SDK versions.
