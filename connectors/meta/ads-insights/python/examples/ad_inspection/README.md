# Meta ad inspection Flow (Python)

This runnable Flow reads one ad with the static `meta-ads-insights` connection and stores the result in a registered Attribute. Configure the Marketing API access token in Dex Web using the [connector guide](../../../README.md).

From this Python package, run `dexcli visualize examples/ad_inspection/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/meta-ads-python-fdg --open=false --python .venv/bin/python`, then run `uv run python -m examples.ad_inspection.worker`. Start `MetaAdsInsightsAdInspectionPython` with `{"adId":"123456789012345"}` and a unique Flow ID. The mock-backed real Dex integration test is `uv run pytest examples/ad_inspection/integration_test.py`. Live Meta authorization remains unverified.
