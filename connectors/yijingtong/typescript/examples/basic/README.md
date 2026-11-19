# Yijingtong warehouse example

This Flow reads the authorized warehouse list and persists it. Its static connection is `yijingtong`.

Start `dexcli dev -open=false`, configure the connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. The Start program submits an empty input because the tenant is selected by the connection.
