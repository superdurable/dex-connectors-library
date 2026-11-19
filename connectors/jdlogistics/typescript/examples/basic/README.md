# JD Logistics TypeScript example

This Flow calls `queryWaybillTrace` with static connection name `jdlogistics` and stores the selected result in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- '{"waybillNo":"JDX009866825381"}'` in another terminal.

Use `dexcli dev -open=false` for local testing. Credentials stay in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow.
