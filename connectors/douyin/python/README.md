# Douyin mini app Python connector

`dex-connector-douyin` provides the `getBehaviorSummary` Query for one completed mini app reporting day. It obtains a non-service-provider `client_token` with the app's `client_key` and `client_secret`, then reads only `data.sum` from the [official behavior analysis API](https://developer.open-douyin.com/docs/resource/zh-CN/mini-app/develop/server/operation/data-analysis/user-analysis/behavior-analysis-datav3). Results contain aggregate active, new, total, opening, and share counts; raw behavior rows, tokens, and provider error text stay out of Flow output.

## Connection

Sign in to the [Douyin developer console](https://developer.open-douyin.com/), create or select your own mini app, then open **小程序控制台 → 开发 → 开发配置**. Copy the non-secret AppID/client_key into `clientKey` and matching secret AppSecret/client_secret into `clientSecret` in Dex Connections. Blank credentials prevent requests. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for the displayed defaults. The connector caches access tokens only in Worker memory until one minute before expiry; an expiry response discards the token before retry. Service-provider merchant delegation is outside this connector.

Supply a `date` in `YYYY-MM-DD` format within the last 90 days, excluding today. Before 10:00 China Standard Time, yesterday is also excluded while Douyin prepares its report. The connector uses a UTC+08:00 day to form Unix-second `start_time` and `end_time`; the official behavior page does not explicitly identify the analytics timezone, so this Go-compatible interpretation needs live verification. There are no operation configuration fields.

See `examples/get_behavior_summary/` for a runnable Dex Flow and Worker. Run `uv run pytest tests` to replay fixed-clock Go-recorded HTTP scenarios. Live Douyin authorization and report values were not verified without a user-controlled connection.
