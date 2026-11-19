# Douyin TypeScript connector

`@superdurable/connector-douyin` provides the read-only `getBehaviorSummary` Query for a completed China Standard Time day of mini app activity. It returns aggregate active, new, and total user counts plus open and share counts; individual user behavior and provider error detail stay out of Flow results.

In the [Douyin Open Platform](https://developer.open-douyin.com/), open **小程序控制台 > 开发 > 开发配置** for your mini app. Copy its AppID/client_key and secret AppSecret/client_secret into Dex Connections. Blank values prevent requests. Leave timeout and maxResponseBytes blank for displayed defaults. Query `date` is `YYYY-MM-DD` within 90 days; yesterday's report becomes available after 10:00 China Standard Time.

The [runnable Flow](examples/basic/) uses static connection name `douyin`. `npm run check` replays 17 Go-recorded scenarios covering token acquisition, reporting, Retry, date validation, and filtered output. Live provider authorization remains unverified without a user-controlled mini app.
