# Azure DevOps Python project discovery Flow

This runnable Flow calls `listProjects` through the static `azure-devops` connection, stores one bounded project page in a Dex Attribute, and completes with the typed result. Configure the organization URL and PAT as described in the [connector README](../../../README.md).

Run `dexcli dev`, configure the connection in Dex Web, and start the Worker with `uv run --project connectors/microsoft/azure-devops/python python -m examples.project_discovery.worker`. Start Flow type `AzureDevOpsProjectDiscovery` with a unique Flow ID and input `{"top":20,"skip":0}`. Increase `skip` by `top` to read another page. The Worker has no inbound listener. Optional branches are unwired and fail the Flow when selected.

For deterministic real-Dex verification, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run --project connectors/microsoft/azure-devops/python pytest connectors/microsoft/azure-devops/python/examples/project_discovery/integration_test.py`. Live provider authorization remains unverified.
