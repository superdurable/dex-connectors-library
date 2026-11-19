# Jiandaoyun application directory Flow

The runnable [`flow.py`](flow.py) calls `listAppsPage` on the static `fanruan` connection and records only application IDs and names in a Dex Attribute. Set a user-controlled Jiandaoyun API Key in Dex Web Connections using the connector's authorization guide. Start [`worker.py`](worker.py), then run `python -m examples.app_directory.start <unique-flow-id> --skip 0 --limit 100` from the Python package directory. Omit page flags for the first page at the provider's default page size. A full page provides `nextSkip` for a subsequent Flow.

Generate and lint the strict FDG 2.0 graph with `dexcli visualize examples/app_directory/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/fanruan-apps-python-fdg --python .venv/bin/python --open=false`.

The integration test starts a real Dex Worker and Client, injects a fake HTTPS provider, checks a retry and the filtered persisted result. It needs a running local `dexcli dev -open=false` server. Live Jiandaoyun authorization and application scope remain unverified without a user-controlled key.
