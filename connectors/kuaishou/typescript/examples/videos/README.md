# Kuaishou video-list example

This Flow reads one page of the authorized user's videos. Its static connection is `kuaishou`.

Start `dexcli dev -open=false`, configure the `kuaishou` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. The start script requests the first page with a blank cursor; use a prior result's `nextCursor` to request the next page.
