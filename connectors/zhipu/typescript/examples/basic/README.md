# Zhipu text generation example

This Flow requests one nonstreaming text answer and persists the result. Its static connection is `zhipu`.

Start `dexcli dev -open=false`, configure the connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. The Start program accepts an optional JSON argument such as `{"prompt":"hello","maxOutputTokens":64}`.
