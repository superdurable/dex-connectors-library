# Polyv connector

Polyv is operated by [易方信息科技股份有限公司](https://www.polyv.net/agreement/). Its official [channel-list contract](https://help.polyv.net/live/api/account/channels/), [signing contract](https://help.polyv.net/live/api/buildSign/), and [key guide](https://help.polyv.net/live/api/getSecretKey/) document this Query. The connector calls `GET https://api.polyv.net/live/v3/user/channels` with a fresh 13-digit millisecond timestamp and an uppercase MD5 signature over sorted nonempty parameters. Only channel IDs are returned; no channel password, watch URL, or viewer information enters the Flow.

## Connection

Sign in to the [Polyv live console](https://console.polyv.net/live/index.html#/develop/app-id), open **Development Settings > App ID**, and copy AppId and AppSecret to Dex Web Connections as `app_id` and secret `app_secret`. The AppSecret is used only for signing and never sent in the request. Leave timeout and response-size limit blank for the displayed defaults. Polyv requires the timestamp to be within three minutes; keep the Worker clock synchronized. Account permissions determine visible channels. The provider documents a frequency limit but does not publish a fixed value on the channel-list page.

`listChannels` takes no input. It selects `listed` for a valid list (including empty), `providerRejected` for definite provider denial, `invalidResponse` for malformed or oversized data, and retries rate limits, temporary server failures, and transport failures. The response is bounded to the configured byte limit.

## Example

The [channel catalog Flow](go/examples/channel-catalog/) is a runnable Worker with static Dex connection name `polyv`. Its mock uses synthetic channel IDs and is verified against a real Dex Worker and Client. Live Polyv authorization and channel-list behavior require a user-controlled connection and remain unverified here.
