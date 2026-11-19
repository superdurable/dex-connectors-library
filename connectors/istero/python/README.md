# IsTero Python connector

This package adds the read-only `getAccountStatus` Query for the configured ISAS account. Configure the secret API key and numeric ISID using the [company guide](../README.md). The operation-specific `new_get_account_status_step` returns the account ID, calls used today, and remaining daily allowance. Its `retrieved` branch is the runnable example's happy path; `providerRejected`, `invalidResponse`, and `defect` are optional.

Run `uv sync --locked` and `uv run pytest` in this directory. The [account status Flow](examples/account_status/) includes a Worker entrypoint and a real Dex integration test with a mock provider. Live ISAS authorization has not been verified.
