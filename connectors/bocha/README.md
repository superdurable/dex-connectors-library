# Bocha AI connector

This connector searches public web pages through [Bocha's official Web Search API](https://open.bochaai.com/). It sends `POST https://api.bochaai.com/v1/web-search` with a Bearer API Key and returns up to five page titles, URLs, and short snippets. It excludes long summaries, icons, raw provider messages, and the Key from Flow data. Bocha AI is operated by [杭州博查搜索科技有限公司](https://open.bochaai.com/privacy-policy).

Create an API Key at the [Bocha Open Platform](https://open.bochaai.com/api-keys), save it in Dex Web Connections, and start `SearchWeb` with a nonblank `query`. This is a read-only search that may consume provider credit. Retry is limited to one additional attempt because a successful response lost in transit may lead to a duplicate billed call. The connector disables redirects and bounds provider responses.

Run the [example Flow](go/examples/search-web/) using its generated mock or a user-controlled connection. Live search and billing behavior are unverified without a user-controlled Key.
