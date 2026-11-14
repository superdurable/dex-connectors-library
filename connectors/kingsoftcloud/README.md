# Kingsoft Cloud IAM

The Go connector reads one page from [IAM ListUsers](https://docs.ksyun.com/documents/2?type=6). It returns only `UserId` and `UserName`; contact and profile fields in the provider response are discarded. The optional `Marker` input continues from `NextMarker` when `HasMore` is true. `MaxItems` is 1–100; zero uses the provider's 100-user default. The connector makes one request per Flow Step and does not automatically scan all pages.

IAM requests use Kingsoft Cloud's documented simplified HMAC-SHA256 signature. The access key ID and secret access key are Dex credentials. The UTC timestamp and signature are generated for each request. The response never contains credentials or personal contact details. [Signing specification](https://docs.ksyun.com/documents/40299) and [public parameters](https://docs.ksyun.com/documents/1406) define the wire contract.

Use the [runnable Dex Flow](go/examples/user-list/README.md) to configure and verify a connection. Live IAM authorization requires customer-owned credentials in Dex Web and has not been exercised by the mock tests.
