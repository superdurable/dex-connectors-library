# Resend connector for Python

Install with `pip install dex-connector-resend==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `sendEmail` (Mutation) and `getEmail` (Query) with the same
branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `sendEmail` | `new_send_email_step` | `sent` | `providerRejected`, `invalidResponse`, `defect` |
| `getEmail` | `new_get_email_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`sendEmail` sends the stable Dex call ID in `Idempotency-Key`. Resend keeps
keys for 24 hours; the default one-hour retry budget stays within that window,
so keep any per-Step retry override shorter than 24 hours. A 409
`concurrent_idempotent_requests` retries with the same key, while
`invalid_idempotent_request` selects `providerRejected`. 408, 429, 5xx, and
transport failures retry; other 4xx responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `resend` as described in the
[connector README](../README.md#authorization). The API key travels only in the
`Authorization` header and is resolved anew for every attempt.

## Example

[`examples/welcome_email`](examples/welcome_email/flow.py) checks the request,
sends it with `new_send_email_step`, and records the Resend email ID. Run the
Worker with `uv run python -m examples.welcome_email.worker` after
`dexcli dev` has loaded a project containing the `resend` connection.

## Tests

`resendmock.Mock` scripts both operations and defaults to the manifest mock
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
