# EMQX Cloud connector

This connector calls the [EMQX Cloud Platform API](https://docs.emqx.com/en/cloud/latest/api/deployment_manage.html) `GET /deployments` endpoint. It lists deployments visible to a Platform API key's selected projects and returns IDs, names, plans, capacity, project, region, status, version, provider, and creation time. The response is bounded by `maxResponseBytes` (1 MiB by default). The provider limits this endpoint to 60 calls per 60 minutes.

## Connection

Sign in to the [EMQX Cloud Console](https://cloud.emqx.com/console/) as a Root User or Administrator. Open **Settings > Platform API Key**, create a key with **Deployment Management** functionality and only the project scopes needed, then copy the Platform API Key and Secret shown once into the corresponding Dex secret fields. Copy the Platform API endpoint ending in `/public_api/v1` to `platformEndpoint`. It must be an absolute HTTPS URL with no URL credentials, query, fragment, or trailing slash. Do not use a Broker Deployment App ID or App Secret. Revoke the key on the same Console page.

## Go

The [deployment list example](go/examples/deployment-list/) runs a Worker with static Dex Web connection name `emq`. The Flow uses `emq.NewListDeploymentsStep` and completes on `listed`. Other branches are optional and fail the Flow unless wired to a business response. Transient transport failures, HTTP 408/429, and 5xx retry under the default Query policy.

No live EMQX Cloud key is bundled. The mock example exercises the actual Dex Worker and Server; a real provider call requires saving your own connection in Dex Web.

## Python

The [Python deployment list example](python/examples/deployment_list/) uses the same static connection and verifies retry through a real Dex Worker and Client. The package replays the Go-recorded provider scenarios. Live EMQX Cloud authorization and deployment data remain unverified without a user-controlled connection.
