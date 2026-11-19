# Doc2X Python connector

`dex-connector-doc2x` provides the `getParseStatus` Query for an existing PDF parse task. It sends `GET /api/v2/parse/status?uid=...` to the fixed [Doc2X v2 endpoint](https://doc2x.noedgeai.com/help/zh-cn/api/doc2x-api-v2-pdf-interface.html) and returns only `status` and `progress` (percent, 0–100). PDF text, result URLs, and provider failure detail do not enter the result or failure.

## Connection

Sign in to the [Doc2X Open Platform](https://open.noedgeai.com), open **API Keys**, create or select a platform API Key, and copy it to the secret `apiKey` field in Dex Connections. Blank prevents requests; a desktop login token is not an API Key. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults; they are a Go duration and byte limit. There are no operation configuration fields. Use the `uid` returned by the PDF preupload API as the Query input.

See `examples/get_parse_status/` for a runnable Dex Flow and Worker. Run `uv run pytest tests` to replay Go-recorded HTTP scenarios. Live Doc2X authorization and PDF task state were not verified without a user-controlled connection.
