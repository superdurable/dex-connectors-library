# Instagram media listing Flow (Python)

This runnable Flow reads one media page with the static `instagram-business` connection and stores the result in a registered Attribute. Configure the User token in Dex Web using the [connector guide](../../../README.md).

From this Python package, run `dexcli visualize examples/media_list/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/instagram-python-fdg --open=false --python .venv/bin/python`, then run `uv run python -m examples.media_list.worker`. Start `InstagramBusinessMediaListPython` with `{"igUserId":"17900000000000001"}` and a unique Flow ID. Use `nextCursor` as `after` for the next page. The mock-backed real Dex integration test is `uv run pytest examples/media_list/integration_test.py`. Live Meta authorization remains unverified.
