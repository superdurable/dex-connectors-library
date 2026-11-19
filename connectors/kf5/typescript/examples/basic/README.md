# KF5 TypeScript example

This Flow calls `getTicket` with static connection name `kf5` and stores the selected ticket status in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- '{"ticketID":123}'` in another terminal.

Use `dexcli dev -open=false` for local testing. Credentials stay in the Dex connection store, outside Flow input and state. The optional `noData` and rejection branches are unwired and fail the Flow.
