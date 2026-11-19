# WxPusher Python connector

`dex-connector-wxpusher` exposes the `getSubscriberCount` Query Step at `dex_connectors.wxpusher`. See the [connector manifest](../connector.yaml) for provider authorization, configuration, and branch behavior.

The [runnable Flow](examples/subscriber_count/) uses the static `wxpusher` connection and stores only non-secret get subscriber count results. Its integration test runs a real Dex Worker/Client with a fake provider. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/wxpusher-python-fdg --python .venv/bin/python examples/subscriber_count/flow.py
```
