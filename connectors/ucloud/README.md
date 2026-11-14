# UCloud connector

The Go connector calls UCloud's [GetRegion API](https://docs.ucloud.cn/api/uaccount-api/get_region) to list regions and availability zones accessible to the signing account. It exposes the `GetRegion` Query Step and a runnable [`region-list` Flow](go/examples/region-list/README.md). GetRegion has no operation input; the account key determines the result.

Create a matched AccessKey ID (PublicKey) and AccessKey Secret (PrivateKey) in [UCloud API Keys](https://console.ucloud.cn/uaccount/api_manage). Dex stores both as secret connection credentials. The connector constructs the [official signature](https://www.ucloud-global.com/en/docs/api/summary/signature) by sorting parameter names, concatenating each name and value, appending PrivateKey, then SHA-1 hex encoding. It sends Action, PublicKey, and Signature in a POST JSON body over HTTPS. PrivateKey is never sent. Provider messages, transport URLs, and credentials are not copied into results or failures.

The Query retries HTTP 408, 429, 5xx, transport failures, and UCloud RetCode 110, 130, 150, and 153 under Dex's default retry budget. Other nonzero RetCodes select `providerRejected`, with authentication and authorization kinds where documented. Malformed or oversized responses select `invalidResponse`; missing or malformed credentials select `defect`. A success response without `Regions` yields an empty list, as the field is optional in UCloud's contract.

Install `github.com/superdurable/dex-connectors-library/connectors/ucloud/go@v0.1.0` after release. Provider tests use an HTTP test server. The mock-backed example integration test uses a real Dex Worker and Client. Live key-pair authorization and actual region visibility remain unverified without a user-configured account.
