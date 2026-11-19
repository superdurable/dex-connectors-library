# Qiniu Kodo bucket list (Python)

Create a connection named `qiniu` in Dex Web using the authorization guide in `connector.yaml`. Enter the paired secret AccessKey and SecretKey; leave connection defaults blank unless an approved proxy requires an override. Optional outcomes are unwired and fail the Flow when selected.

From `connectors/qiniu/python`, run `uv sync --locked`, start `dexcli dev -open=false`, and run `uv run python -m examples.bucket_list.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
dexcli visualize examples/bucket_list/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-qiniu-python-buckets-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing to the local Dex server, `uv run pytest examples/bucket_list/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic 503, and checks the persisted bucket names. The test uses no real Qiniu keys; live authorization remains unverified.
