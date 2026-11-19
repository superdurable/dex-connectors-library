# EMQX Cloud deployment list

`DeploymentListFlow` calls `listDeployments` through the static `emq` Dex Web connection, persists the deployment list Attribute, and completes with it. The optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. Temporary failures retry under the Dex Step policy, but the provider permits only 60 calls per 60 minutes.

Create a connection named `emq` in Dex Web. Sign in to the [EMQX Cloud Console](https://cloud.emqx.com/console/) as Root User or Administrator, open **Settings → Platform API Key**, create a key with Deployment Management and the needed project scope, and copy its Key and one-time Secret into `platform_api_key` and `platform_api_secret`. Copy the same page's Platform API endpoint ending in `/public_api/v1` into `platformEndpoint`. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for their displayed defaults. There are no operation fields.

From `connectors/emq/python`, run `uv sync --locked`, start `dexcli dev -open=false`, and run `uv run python -m examples.deployment_list.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/deployment_list/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-emq-python-deployments-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/deployment_list/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic provider 503, and checks the filtered list. It uses no real Platform key. Live EMQX Cloud authorization and deployments remain unverified.
