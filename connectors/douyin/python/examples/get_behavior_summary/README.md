# Douyin mini app behavior summary

`GetBehaviorSummaryFlow` calls `getBehaviorSummary` through the static `douyin` Dex Web connection, persists aggregate counts, and completes with them. The optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. A temporary failure retries under the limited Step policy. The Worker caches the client token in memory; an expired-token response clears it before retry.

Create a connection named `douyin` in Dex Web. At the [Douyin developer console](https://developer.open-douyin.com/), select your own mini app, then open **小程序控制台 → 开发 → 开发配置**. Copy AppID/client_key into non-secret `clientKey` and matching AppSecret/client_secret into secret `clientSecret`. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for displayed defaults. There are no operation fields. Submit a `date` in `YYYY-MM-DD` within the last 90 days and before the latest completed report day; yesterday is allowed only after 10:00 UTC+08:00.

From `connectors/douyin/python`, run `uv sync --locked`, start `dexcli dev`, and run `uv run python -m examples.get_behavior_summary.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/get_behavior_summary/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-douyin-python-behavior-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/get_behavior_summary/integration_test.py` starts the Flow through a real Worker and Client. It verifies one token request, retry of a synthetic report 503, and filtered aggregate output. It uses no real Douyin credentials. Live authorization, report values, and the Go-compatible analytics day timezone remain unverified.
