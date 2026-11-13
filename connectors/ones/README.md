# ONES connector

[ONES](https://ones.cn/aboutus) is operated by 深圳复临科技有限公司. This Go connector calls the [ONES Open API v2 team-list endpoint](https://docs.ones.cn/developer/openapi/api/01-get-organization-teams/) to list teams accessible to the connected user. It returns only team UUIDs, names, and numeric statuses, omitting owner identifiers and timestamps.

## Connection

Sign in to your ONES tenant from [ONES](https://ones.cn/). In **Personal Settings > Personal API keys**, create a key with `read:account:teams` scope and copy its one-time-shown secret into Dex `api_key`. The [official key guide](https://docs.ones.cn/developer/openapi/auth/personal-api-keys/) describes the path and Bearer authentication. Set `baseUrl` to the HTTPS origin of your ONES tenant, without a path. Leave timeout and response limit blank for displayed defaults. Revoke a compromised key in Personal API keys and create a replacement. The key represents its creating user and is suited to user-level integrations.

## Go

The [team catalog Flow](go/examples/team-catalog/) is a runnable Worker with static Dex Web connection name `ones`. `listed` completes the Flow; optional branches fail unless an application wires a business response. Transient failures retry safely. The local mock integration uses synthetic team data and a real Dex Worker and Client. Live authorization and team visibility remain unverified without a user-controlled tenant connection.
