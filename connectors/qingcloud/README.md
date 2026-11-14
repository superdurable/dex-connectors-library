# QingCloud connector

The Go `describeZones` Query uses QingCloud's [IaaS DescribeZones API](https://docsv4.qingcloud.com/user_guide/development_docs/api/api_list/other/describe_zones/) to list at most 100 accessible zones with their `active`, `faulty`, or `defunct` status. It is read only. The request includes a fresh UTC timestamp, fixed protocol parameters, and an [HmacSHA256 signature](https://docsv4.qingcloud.com/user_guide/development_docs/api/rules/signature/) over the sorted RFC 3986 query. Only the Access Key ID and signature are sent over HTTPS; the Secret Access Key remains in the connection credential.

## Connect

Sign in at the [QingCloud console](https://console.qingcloud.com), then open **Products & Services → Access & Authorization → API Keys**. Create an enabled key, preferably for a scoped subaccount or IAM user, and optionally restrict Worker egress with an IP whitelist. Copy its ID to `accessKeyId` and the paired secret to `secretAccessKey` in Dex Web Connections. Blank values prevent calls. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Delete and recreate a compromised key in the same console page. `DescribeZones` needs no zone input.

HTTP 429, server failures, QingCloud server codes `5000`, `5100`, `5200`, `5300`, and transport failures retry. Definite provider codes such as authentication failure `1200`, expired timestamp `1300`, and quota `2500` select optional `providerRejected`; malformed or oversized JSON selects optional `invalidResponse`. The example leaves optional branches unwired, so these outcomes fail the Flow.

The runnable example is in [`go/examples/zone-read`](go/examples/zone-read). Provider tests use a local HTTP server and the official signature vector. Real QingCloud key authorization and live zone discovery remain unverified without a user-controlled account.
