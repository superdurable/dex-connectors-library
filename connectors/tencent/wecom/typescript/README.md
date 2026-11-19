# WeCom TypeScript connector

`@superdurable/connector-wecom` implements `sendGroupRobotMessage` and `sendApplicationMessage` as fenced Dex Mutations. Each uses its own connection method. The robot operation can repost an unknown outcome up to the send cap, while the application operation uses WeCom's four-hour duplicate check and stops automatic resend at 3 hours 50 minutes after the first attempt.

`examples/basic/` has runnable real Dex Worker and Client Flows for both operations. Run `npm run check` for all 22 Go-recorded provider scenarios, and `npm run test:integration` against `dexcli dev -open=false` for real Worker, Client, retry, and persistence verification. Live WeCom authorization is unverified without a user-controlled connection.
