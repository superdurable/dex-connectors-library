# Amazon S3 Connector

The Amazon S3 Connector lists, inspects, reads, and stores objects in Amazon S3
and in S3-compatible stores, such as Cloudflare R2 and MinIO, through the S3 REST
API. It exposes operation-specific Dex Step factories:

| Operation | Kind | S3 request | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- | --- |
| `listObjects` | Query | `GET /?list-type=2` (ListObjectsV2) | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `headObject` | Query | `HEAD /{key}` | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getObjectText` | Query | `GET /{key}` | async | `read` | `notFound`, `unsupportedContent`, `tooLarge`, `providerRejected`, `invalidResponse`, `defect` |
| `putObject` | Mutation | `PUT /{key}`, and `HEAD /{key}` after a refused create-only write | async | `stored` | `alreadyExists`, `providerRejected`, `defect` |

Most Flows use object storage to find an artifact by name or archive one, so
`listObjects` is the resolver: it pages a bucket by prefix and delimiter and
never reports an empty listing as success. The connector does not delete, copy,
or tag objects, and this release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Authentication

A connection holds an access key ID, a secret access key, and, for temporary
AWS credentials only, a session token. All three are stored as secrets. Every
request is signed with AWS Signature Version 4 for the configured `region`;
the secret access key never leaves the Worker, and neither it nor the session
token appears in a URL, a Result, a Failure, a receipt, or a log. Credentials
are reread before every call, so a replaced key takes effect without a restart.

- **Amazon S3**: create an IAM user only for Dex at
  <https://console.aws.amazon.com/iam> > **Users** > **Create user**, attach an
  inline policy that allows `s3:ListBucket` on `arn:aws:s3:::your-bucket` and
  `s3:GetObject` and `s3:PutObject` on `arn:aws:s3:::your-bucket/*`, then open
  **Security credentials** > **Access keys** > **Create access key**, choose
  **Other**, and copy both values from **Retrieve access keys**; AWS never shows
  the secret again. Long-term IDs start with `AKIA`.
- **Cloudflare R2**: <https://dash.cloudflare.com> > **R2 object storage** >
  **Account Details** > **API Tokens** > **Manage** > **Create Account API
  token** with **Object Read & Write** scoped to the bucket. Copy the Access Key
  ID and Secret Access Key shown once, set `endpoint` to
  `https://<ACCOUNT_ID>.r2.cloudflarestorage.com` and `region` to `auto`.
- **MinIO**: a user or service account whose policy is limited to the bucket;
  set `endpoint` to the server's API address.
- **Temporary credentials**: an `ASIA...` key with a session token, such as the
  output of `aws sts get-session-token`. They expire, usually within hours, and
  the connector cannot renew them, so an expired token selects
  `providerRejected` until all three values are replaced.

IAM roles, EC2 instance profiles, ECS task roles, IAM Identity Center (SSO), and
`AssumeRole` chaining are out of scope for v0.1.0: Dex Web connections hold
static secrets, and this release reads no AWS shared configuration, environment
credentials, or instance metadata.

`s3:GetObject` also lets create-only `putObject` recognize its own earlier
attempt, and `s3:ListBucket` lets `headObject` report a missing key as
`notFound`; without it Amazon S3 answers 403, which selects `providerRejected`.

For local Dex Web setup, name the factory connection and load the same name at
application startup, as [`examples/report-archive/main.go`](examples/report-archive/main.go)
does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := s3.NewLocalConnection(store, reportarchive.ConnectionName)
```

Hosted applications resolve operation-scoped credentials with
`hostedconfig.NewCredentialProviderFromEnvironment(s3.ConnectorID, name,
s3.DecodeResolvedCredentialsJSON)`, which accepts exactly `access_key_id`,
`secret_access_key`, and an optional `session_token`.

## Endpoint, Region, and addressing

A blank `endpoint` uses the Amazon S3 Regional endpoint
`https://s3.<region>.<partition suffix>`, with the suffixes of the AWS SDK's
partition table: `amazonaws.com` for the standard and GovCloud partitions,
`amazonaws.com.cn` for China, and `amazonaws.eu` for the European Sovereign
Cloud. ISO-partition Regions set `endpoint` explicitly. A custom endpoint must
use HTTPS, except a loopback HTTP address such as `http://127.0.0.1:9000` for a
store on the Worker's own host; user information, queries, and fragments are
rejected, and a path is kept as a prefix.

`addressingStyle` is `auto` by default: virtual-hosted requests
(`https://bucket.s3.eu-west-1.amazonaws.com/key`) for Amazon S3, path-style
requests (`https://minio.example.com/bucket/key`) for a custom endpoint, and
path-style for a bucket name with a dot, because the Amazon S3 wildcard
certificate cannot match it. Amazon S3 documents that it supports path-style
URLs in all Regions today and will discontinue them at a date it has not
announced. `virtualHosted`
with a dotted bucket over HTTPS selects `defect`. Redirects are never followed:
a wrong Region selects `providerRejected`, and when S3 reports the bucket's
Region in `x-amz-bucket-region` the Failure says which Region to set.

Bucket names follow the general purpose bucket rules (3 to 63 lowercase
letters, digits, dots, and hyphens, not an IP address); legacy `us-east-1`
names with uppercase letters or underscores are rejected. Keys are 1 to 1024
bytes of UTF-8 without control characters or `.` and `..` path segments, which
HTTP clients and proxies may normalize away. A blank operation `bucket` uses
the connection's `defaultBucket`.

## Why the official signer, not the full AWS SDK

The connector depends only on the core module `github.com/aws/aws-sdk-go-v2`
v1.43.0, whose `go.mod` declares `go 1.24` and requires only
`github.com/aws/smithy-go`. Both modules are already in the module graph of
`sdkgo` v0.18.0, so a consuming application downloads nothing new. Signing,
the subtle part, comes from the SDK's own `aws/signer/v4`, and the tests
reproduce the three worked signatures (GET Object, PUT Object of
`test$file.text`, and list objects) from the Amazon S3 API Reference page
"Examples: Signature Calculations", as the Internet Archive captured it on
2025-01-02, because the page now redirects to the API Reference index.

The full client, `github.com/aws/aws-sdk-go-v2/service/s3` v1.106.0, also
builds with Go 1.24, but it adds 9 modules (`service/s3`, its four
`service/internal/*` helpers, `internal/v4a`, endpoint rules, configuration
sources, and event streams) and, in a minimal program, about 5.6 MB of binary
(11.1 MB against 5.5 MB with only the signer). It would also have to be
configured against this repository's rules: it makes up to three attempts per
call on its own, which hides attempts from Dex; it adds default request
checksums to PutObject, which some S3-compatible stores reject; it deserializes
responses without the connector's size bounds; and its errors carry provider
message text. Sending the four REST requests directly keeps every response
bounded, every redirect unfollowed, every retry owned by Dex, and every header
explicit.

## Operations

Every operation bounds its requests by 25 seconds in total and each exchange
by 12 seconds, below the 30-second Execute timeout. Transport failures, 408,
429, 503 `SlowDown`, other 5xx except 501, 400 `RequestTimeout`, 307
`TemporaryRedirect`, and 409 `ConditionalRequestConflict` or `OperationAborted`
return Retry, honoring `Retry-After` up to one hour. 401, 403, 400, 404 for a
bucket, 501, and other 4xx select `providerRejected`. Failures name the HTTP
status and the S3 error code, such as `HTTP 403 SignatureDoesNotMatch`, never
the error message text. Receipts carry the Call ID, `bucket/key`, and the
`x-amz-request-id` response header.

### listObjects

`ListObjectsInput` sets `Prefix`, `Delimiter`, `StartAfter`, and
`ContinuationToken`; `MaxKeys` is 1 to 1000, and zero uses the connection's
`listPageSize`. Every request asks for `encoding-type=url` so that any key
crosses the XML response, and keys are decoded only when the store echoes
`EncodingType` (`+` is a space). A page holds `Objects` (key, size, quoted
ETag, modification time, storage class) and, with a delimiter,
`CommonPrefixes`. `found` means the page has entries or `IsTruncated` is set;
pass `NextContinuationToken` back to read the next page. `notFound` is
selected only for an empty final page, so an empty listing is never a silent
success. A missing bucket (`NoSuchBucket`) selects `providerRejected`, not
`notFound`. A page larger than `maxResponseBytes`, a truncated page without a
token, or an invalid document selects `invalidResponse`.

### headObject

Returns `ObjectMetadata`: size, ETag exactly as S3 sends it (with quotes),
content type, encoding, language, disposition, cache control, modification
time, version ID, storage class (Amazon S3 reports none for `STANDARD`),
server-side encryption, and user-defined metadata keyed by lowercase name
without `x-amz-meta-`. S3 returns a non-ASCII metadata value RFC 2047-encoded,
and the connector keeps it as returned. A HEAD response has no body, so a
missing key and a missing bucket both select `notFound`. Metadata values that
repeat the connection's secrets are dropped.

### getObjectText

Decides from the response headers before reading any content: a declared
`Content-Encoding` such as `gzip`, or a content type that is not text, selects
`unsupportedContent`, and a `Content-Length` above `maxTextBytes` selects
`tooLarge`. Text types are `text/*`, JSON, XML, YAML, NDJSON, CSV, TOML,
JavaScript, SQL, and `+json`, `+xml`, or `+yaml` types; an unlabeled object
(`application/octet-stream`, `binary/octet-stream`, or none) is accepted when
its bytes are valid UTF-8 without a NUL byte. A chunked body is still read only
up to `maxTextBytes`. The text is returned unchanged, including any byte order
mark. Content that repeats the connection's secrets selects `invalidResponse`
instead of entering Flow state. `NoSuchKey` selects `notFound`; `NoSuchBucket`
selects `providerRejected`.

### putObject and duplicate dispatch

`PutObjectInput` holds the key, a required `ContentType`, at most one of
`TextContent` and `ByteContent` (neither stores an empty object), up to
`maxUploadBytes` of content (at most 5 MiB, because the content travels through
Flow state), and user-defined `Metadata` with lowercase names and printable
ASCII values within the S3 2 KB limit, counted conservatively with each
`x-amz-meta-` prefix and the connector's marker. Every request sends the
content's SHA-256 in `x-amz-content-sha256` and its MD5 in `Content-MD5`, so S3
rejects corrupted content; Object Lock buckets require one of these checksums.

Every object also carries `x-amz-meta-dex-idempotency-key`, the Step
execution's idempotency key, which the SDK derives from the stable Call ID.
Every attempt of one Step execution therefore sends byte-identical content,
headers, and marker; a new Step execution gets a new key. The
`dex-idempotency-key` metadata name is reserved.

Amazon S3 adds an object only whole and, without a precondition, replaces the
object at a key. A repeated PUT of the same bytes leaves the same object, so
`putObject` keeps the async durability default and declares no `uncertain`
branch: a timeout, dropped connection, 5xx, or a response that arrives after
Dex dispatched the Step again is simply retried. The example's real-Dex test
delays the first PUT's response past the seven-second async local phase, sees
Dex send the PUT a second time, and finds one object with the same bytes.

With `IsCreateOnly`, the PUT sends `If-None-Match: *`. S3 then stores the
object only when no current object exists at the key and answers 412 otherwise.
After a 412 the connector sends one HEAD request: an object that carries this
Step execution's marker is the earlier attempt's write and is reported as
`stored` with `IsFromEarlierAttempt`; any other object selects `alreadyExists`
with that object's ETag, version, size, and content type, and nothing is
written. A HEAD that finds no object retries the write; a HEAD that S3 denies
selects `providerRejected`, because without `s3:GetObject` the connector cannot
tell its own write from another writer's. A 409 `ConditionalRequestConflict`
returns Retry, as S3 documents. A store that answers `NotImplemented` selects
`providerRejected` with guidance to leave `IsCreateOnly` false.

Limits of this design:

- Overwrite mode is last-writer-wins. If another writer replaces the object
  between two attempts of the same Step execution, the later attempt writes
  this Step's bytes again. Use `IsCreateOnly` when another writer must never be
  replaced.
- In a versioning-enabled bucket, each repeated overwrite adds an identical
  version, and S3 event notifications fire for each PUT.
- Create-only depends on the store honoring `If-None-Match`. Amazon S3, MinIO
  RELEASE.2025-10-15T17-29-55Z (verified), and Cloudflare R2 (documented in its
  S3 compatibility table, not verified live) do; a store that ignores the
  header overwrites the object and the 200 response cannot reveal it.
- Applications that send multi-megabyte content over slow links can set
  `StepOptionsOverride` durability to sync; the write stays retry-safe either
  way.

## Not in this release

- **`presignGetURL`** was dropped. Presigning is a local computation, which a
  Query could perform, but its output is a bearer credential: Amazon S3
  documents that "presigned URLs are bearer tokens that grant access to those
  who possess them", valid for up to seven days, and a URL signed with a
  session token carries that token in `X-Amz-Security-Token`. A Result is
  durable Flow state that Dex Web displays, and this repository keeps secrets
  out of Results. It needs a platform way to hand a secret to the next Step
  without persisting it.
- **Pickers**: a bucket picker would call ListBuckets, which returns XML and
  requires a SigV4 signature over each request. Studio commands send only
  HTTPS `GET` with a bearer or raw-header credential and accept only a JSON
  object, so bucket names are guided input or `defaultBucket`.
- **Triggers**: Amazon S3 Event Notifications are delivered at least once only
  to Amazon SNS topics, Amazon SQS queues, AWS Lambda functions, and Amazon
  EventBridge, never to an HTTPS webhook. An object-created Trigger needs an SQS
  or EventBridge connector, or an SNS HTTPS subscription, which the endpoint
  must confirm through a handshake before SNS delivers to it; `webhooktrigger`
  has no handshake hook yet.
- Deleting, copying, tagging, version-specific reads, multipart uploads,
  ranged reads, SSE-C, S3 Express directory buckets, access point ARNs, and
  requester-pays buckets.

## Example

[`examples/report-archive`](examples/report-archive) is a runnable Dex Web
**Start Flow** example that generates a Markdown report, archives it once
under `reports/<reportId>.md`, reads its metadata and text back, and lists the
archive folder.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the pinned Dex development server running, the example's real-Dex tests
cover every business outcome and both duplicate-dispatch modes against the
SigV4-verifying fake in `internal/s3fake` (each duplicate test takes about nine
seconds):

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

The live suite runs the connector and the example against a real S3 API. For a
local MinIO server, set its Region so that MinIO checks the signing Region, and
keep its data on a case-preserving, normalization-preserving volume such as
APFS so that non-ASCII keys list back unchanged:

```bash
MINIO_SITE_REGION=us-east-1 MINIO_ROOT_USER=s3connectorlive MINIO_ROOT_PASSWORD=<password> \
  minio server <data-dir> --address 127.0.0.1:9000
S3_CONNECTOR_TEST_ENDPOINT=http://127.0.0.1:9000 S3_CONNECTOR_TEST_REGION=us-east-1 \
S3_CONNECTOR_TEST_BUCKET=dex-s3-live S3_CONNECTOR_TEST_CREATE_BUCKET=true \
S3_CONNECTOR_TEST_ACCESS_KEY_ID=s3connectorlive S3_CONNECTOR_TEST_SECRET_ACCESS_KEY=<password> \
  GOWORK=off go test -tags=live ./... -count=1 -v
```

For Amazon S3, leave `S3_CONNECTOR_TEST_ENDPOINT` blank, name a disposable
bucket, and use a key limited to it. The live tests delete every object they
create with a signed DELETE that the connector itself does not offer.
