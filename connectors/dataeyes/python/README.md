# DataEyes Python connector

`dex-connector-dataeyes` provides the `readWebPage` Query for a public HTTPS page. It posts Markdown-format reads to the fixed DataEyes Reader endpoint. Results contain only `title` and up to 100000 bytes of `markdown`; the submitted URL, provider metadata, and provider error text do not enter results or failures.

## Connection

Get the secret search/reading Key from the [DataEyes Key guide](https://doc.shuyanai.com/doc-8441886): sign in, select **MCP产品 → 接入与密钥**, and create or copy the reading Key. The model API Key cannot call the reader. Enter the reading Key in Dex Connections as `apiKey`. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults (seconds and bytes respectively). Increase them only for an approved slow or large public page. The provider endpoint and Markdown format are fixed.

The `url` input must be a public HTTPS domain URL without user info, query, or fragment. Never submit private or signed links. Reads use provider quota; the manifest limits retries after ambiguous failures. The optional `providerRejected`, `invalidResponse`, and `defect` outcomes fail the Flow unless wired for an application response.

See `examples/read_web_page/` for a runnable Dex Flow and Worker. Run `uv run pytest` to replay the Go-recorded HTTP scenarios. Live DataEyes authorization needs a user-controlled Dex Connection and was not exercised by the recorded fixture.
