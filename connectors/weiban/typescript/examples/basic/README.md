# Weiban tag example

This Flow reads the first authorized tag page and persists the privacy-minimized result. Its static connection is `weiban`.

Start `dexcli dev -open=false`, configure the connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. The Start program submits an empty input because the tenant is selected by the connection.
