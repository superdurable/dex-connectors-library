# EMQX Cloud Python connector

`dex-connector-emq` provides the `listDeployments` Query. It sends a no-input `GET /deployments` through the configured Platform API endpoint and returns the deployments visible in the key's project scope. The [official API](https://docs.emqx.com/en/cloud/latest/api/deployment_manage.html) limits this call to 60 requests in 60 minutes; 429 and temporary failures retry under the Dex Step policy.

## Connection

Sign in to the [EMQX Cloud Console](https://cloud.emqx.com/console/) as a Root User or Administrator. Open **Settings → Platform API Key**, select **Create New Key**, grant **Deployment Management** and only the needed project scope, then copy the Platform API Key and one-time-shown Secret into the secret `platform_api_key` and `platform_api_secret` fields in Dex Connections. Copy the Platform API endpoint ending in `/public_api/v1` from the same page into non-secret `platformEndpoint`. This is the Platform API, not a Broker Deployment API endpoint. Blank values prevent requests. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for their displayed defaults. There are no operation fields.

See `examples/deployment_list/` for a runnable Dex Flow and Worker. Run `uv run pytest tests` to replay Go-recorded HTTP scenarios. Live EMQX Cloud authorization and deployment data were not verified without a user-controlled connection.
