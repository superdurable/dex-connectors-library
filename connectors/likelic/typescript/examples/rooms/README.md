# Likelic room-list example

This Flow reads one page of notification room IDs and push states. Its static connection is `likelic`.

Start `dexcli dev -open=false`, configure the `likelic` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. The start script requests page 1.
