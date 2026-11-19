# ApiZero account-information Flow

Install with `uv sync --locked --project connectors/apizero/python`. Configure
the `apizero` connection in Dex Web Connections with a `sk_live_` API Key from
the [provider console](https://apizero.cn/account/keys). Run
`uv run python -m examples.account_info.worker` from the Python package directory
with `DEX_FLOW_SERVICE_ADDRESS` pointing to
`dexcli dev`. Start `ApiZeroAccountInfo` with `{}`. Only `found` is wired;
other outcomes fail the Flow for inspection.

The integration test runs this Flow against real Dex with a fake provider.
Real ApiZero authorization and account balances remain unverified.
