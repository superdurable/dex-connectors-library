# NetEase Yunxin user-profile Flow

Install with `uv sync --locked --project connectors/netease/python`. Configure the `netease` connection in Dex Web Connections using App Key and App Secret from [Yunxin application management](https://app.yunxin.163.com) > your application > App Key 管理. From the Python package directory, run `uv run python -m examples.user_profile.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `NetEaseUserProfile` with `{"accountId":"sample-user"}` or an existing IM account ID. Only `fetched` is wired; other outcomes fail the Flow for inspection.

The integration test runs this Flow against real Dex with a fake provider. Real Yunxin authorization and profile retrieval remain unverified.
