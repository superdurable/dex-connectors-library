# Gitee repository lookup (Python)

Create a connection named `gitee` in Dex Web using the authorization guide in `connector.yaml`. Its `access_token` is secret; leave the connection defaults blank unless an approved proxy requires an override. The `owner` and `repo` inputs come from the repository URL. Optional outcomes are unwired and fail the Flow when selected.

From `connectors/gitee/python`, run `uv sync --locked`, start `dexcli dev -open=false`, and run `uv run python -m examples.repository_lookup.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
dexcli visualize examples/repository_lookup/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-gitee-python-repository-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing to the local Dex server, `uv run pytest examples/repository_lookup/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic 503, and checks the persisted metadata. The test uses no real Gitee token; live authorization remains unverified.
