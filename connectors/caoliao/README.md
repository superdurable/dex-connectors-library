# Caoliao QR-code directory connector

This connector reads one page of QR-code names and coding identifiers from the [Caoliao OpenAPI V2 QR-code list](https://cli.im/open-api/openapi/v2/api/qrcodes-list.html). It does not read scanned records, member details, or QR-code contents. Caoliao is operated by 宁波邻家网络科技有限公司, as its [official company page](https://cli.im/aboutus) states.

## Connection

Sign in at [Caoliao's Data API page](https://user.cli.im/), open **数据API > 添加配置 > OpenAPI > 开通**, name and save the configuration, then copy the OpenAPI Key from the list. Enter it as the `api_key` secret in Dex Web Connections. The [official authorization guide](https://cli.im/open-api/openapi/v2/auth.html) uses `Authorization: Bearer <API Key>`; the key never enters Flow input or output. Leave transport settings blank for their documented defaults.

`listQRCodes` accepts `pageSize` from 1 to 100 (zero uses the provider default of 20) and the prior response's opaque `nextPageToken` to fetch another page. Blank `pageToken` starts the first page. The [provider error guide](https://cli.im/open-api/openapi/v2/error-codes.html) documents `401` for invalid keys, `429` for rate limits, and `502` for temporary upstream failures. Retries are bounded to three attempts for a person waiting for the page; the operation is read-only although its HTTP method is POST.

The [runnable Dex Flow](go/examples/qrcode-directory/README.md) uses the static `caoliao` connection and a synthetic mock. Live provider authorization and organization visibility require a user-controlled Key and have not been verified here.
