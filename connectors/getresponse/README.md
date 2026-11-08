# GetResponse connector

The Go and Python connectors list one page of contacts with `listContacts` and read one contact with `getContact`. They return only contact ID, email, and name; custom fields and embedded details stay out of Flow results. These queries cover GetResponse SMB accounts. GetResponse MAX needs an additional `X-Domain` header and is outside this initial release.

Open [GetResponse Integrations and API](https://app.getresponse.com/api), generate or copy your API Key, and save it in the Dex Web connection's secret `api_key` field. The connector sends it in the `X-Auth-Token` header with the required `api-key` prefix. Leave `baseUrl` blank for the displayed SMB API default. See [authentication](https://apidocs.getresponse.com/v3/authentication), [contact listing](https://apidocs.getresponse.com/v3/case-study/segments-manual), and [limits](https://apidocs.getresponse.com/v3/limits).

Both reads retry transport failures, HTTP 429, request timeout, and server failures. `getContact` selects `notFound` on a definitive 404. Provider rejections and malformed or oversized responses select optional branches. The [contact-inspection Flow](go/examples/contact-inspection/README.md) is the runnable consumer. The Python package replays the Go-recorded provider scenarios. Live GetResponse authorization remains unverified.
