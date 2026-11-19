# Honor Python connector

`dex-connector-honor` exposes the `lookupAppID` Query Step at `dex_connectors.honor`. It exchanges developer credentials for a short-lived token and looks up an owned Android app by package name, returning only its package name and numeric app ID. Configure Client ID and API key in Dex Web Connections using the [connector guide](../README.md).

The [runnable app ID Flow](examples/lookup_app_id/) uses the static `honor` connection. Its integration test runs a real Dex Worker and Client against a fake HTTP provider, including a 503 retry after token exchange and a persisted result. Live Honor authorization and app lookup remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/honor-python-fdg --python .venv/bin/python examples/lookup_app_id/flow.py
```
