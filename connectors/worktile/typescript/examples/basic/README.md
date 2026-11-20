# Worktile project page example

This Flow reads one zero-based page of tenant projects and persists the result. Its static connection is `worktile`.

Start `dexcli dev -open=false`, configure the connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. Pass `{"pageIndex":0,"pageSize":10}` as the optional argument to the Start program for the first page.
