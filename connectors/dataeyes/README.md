# DataEyes AI connector

This connector reads one public HTTPS page through the [DataEyes Web Reader API](https://doc.shuyanai.com/api-288269246), `POST https://api.shuyanai.com/v1/reader`. It requests Markdown and returns only the page title and bounded Markdown content. The input URL is never copied into the result or failure. DataEyes AI is operated by [海南数眼智能科技有限公司](https://www.shuyanai.com/).

Use a **search/reading Key** from the DataEyes console's **MCP产品 → 接入与密钥** page, as described in the [official Key guide](https://doc.shuyanai.com/doc-8441886). A model API Key is a different credential and does not work. Save the Key in Dex Web Connections and start `ReadWebPage` with an HTTPS public URL without user info, query, or fragment. Reading may consume quota; retry is limited to one additional attempt. The connector disables redirects and maps provider error codes without exposing raw messages.

Run the [Go example Flow](go/examples/read-web-page/) or [Python example Flow](python/examples/read_web_page/) with a user-controlled connection. The Python package replays Go-recorded provider scenarios and its example verifies retry and completion through a real Dex Worker and Client. Live DataEyes reading and quota behavior are unverified without a user-controlled Key.
