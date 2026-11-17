# Box connector

The Go, [Python](python/README.md), and [TypeScript](typescript/README.md) connectors download one bounded Box file by numeric file ID through `downloadFile` and replace an existing file's current content through `uploadFile`. Download returns the ID, Base64 content, and decoded byte count. Upload returns the ID, name, and SHA-1, without retaining content or tokens in its result. The [Go file-download Flow](go/examples/file-download/README.md), [Go file-upload Flow](go/examples/file-upload/README.md), [Python file-download Flow](python/examples/file_download/README.md), and [TypeScript file-download Flow](typescript/examples/file-download/README.md) are runnable consumers.

Go `v0.2.0` also adds `getFile`, `getFolder`, `listFolderItems`, `searchFiles`,
`searchFolders`, `deleteFile`, and `deleteFolder`. All seven use the `safe`
repeat tier. The first five return bounded metadata or offset pages; the
delete operations accept only decimal IDs, delete a file or empty folder,
and return the accepted ID on Box HTTP 204. Box's enterprise settings decide
whether deletion moves an item to trash or removes it permanently. Python and
TypeScript remain on their declared `downloadFile` and `uploadFile` capabilities.

Go `updateFile` and `updateFolder` use absolute PUT fields and the `safe`
repeat tier. Each can rename an item, move it to a parent folder, or create
or change a shared link in one call. Supply a decimal item ID from its Box Web
URL. Leave `name`, `parentId`, or `sharedLinkAccess` blank to preserve that
field; at least one must be set. `parentId` is a decimal folder ID, with `0`
for root. Shared link access is `open`, `company`, or `collaborators`; Box
enterprise policy may restrict these choices. The result includes the current
name and parent ID. Shared link URLs are omitted because they grant access.

Go `shareFile` and `shareFolder` expose sharing as dedicated safe mutations.
Open the selected item in Box Web, copy its decimal ID from `/files/<ID>` or
`/folders/<ID>`, then choose `open`, `company`, or `collaborators` access.
Both operations set an absolute shared link access and return item metadata;
company policies can reject a chosen audience. The
[`file-share` Flow](go/examples/file-share/README.md) exercises the factory
through a real Dex Worker with a provider mock.

Go `createFolder`, `copyFile`, and `copyFolder` use the `limited` repeat tier
with a cap of three possible sends. They require a destination parent ID and
an explicit name. After an unknown result, they list the destination folder
by name before resending. A matching name cannot prove this call created the
item, so the result remains `uncertain`. An unconfirmed result at the cap also
selects `uncertain`.
Box does not expose a provider idempotency key for these POST requests.
Go `createFile` uploads a new file through Box's direct multipart API. Provide
a destination parent ID, unique name, and bounded Base64 content. It uses the
`limited` tier with at most three possible sends; after an unknown result it
lists the destination and compares name, SHA-1, and size before resending. Those
values cannot prove this call uploaded the file; a match selects `uncertain`. A
conflicting name is a definite rejection on the first send and an uncertain
outcome after a possible earlier send. The
[`file-create` Flow](go/examples/file-create/README.md) exercises it through
real Dex with a provider mock.

The Box Web URL after `/files/` or `/folders/` supplies the item ID; `0` is
the root folder and can be listed but cannot be deleted. Search requires text;
`limit` is at most 200 for search and 1000 for folder contents, while `offset`
is zero-based. Leave these inputs at zero to use Box's default page size or
the first page. No item ID or query is a secret.

Create an OAuth 2.0 Platform App in the [Box Developer Console](https://app.box.com/developers/console). In Configuration, enable the `root_readwrite` application scope, register the exact Dex Web redirect URI, and copy the Client ID and secret into the Dex authorization form. Box may require enterprise administrator approval. The user completing consent must have access to the file. Dex stores the access token and each rotated refresh token. See [Box OAuth setup](https://developer.box.com/guides/authentication/oauth2/oauth2-setup/), [scopes](https://developer.box.com/guides/api-calls/permissions-and-errors/scopes/), and the [download endpoint](https://developer.box.com/reference/get-files-id-content/).

The download Query follows only a signed HTTPS `*.boxcloud.com` redirect, without the OAuth token, and selects `invalidResponse` for any other redirect. It retries transport failures, HTTP 429, HTTP 202 (file not ready, honoring `Retry-After`), request timeout, and server failures. HTTP 404 selects the optional `notFound` branch; an oversized file selects `tooLarge`. Upload first reads the file's name and SHA-1; it skips an already identical file, preserving version history. It uses Box's [file-version upload API](https://developer.box.com/reference/post-files-id-content) and reads back ambiguous outcomes. When read-back cannot confirm the content it retries, because the next attempt compares SHA-1 and size before replacing again. Other definitive rejections select `providerRejected`. An unwired optional branch fails the Flow. Box file events remain to be implemented. Live Box authorization and Dex Web setup have not been exercised.

The [Go-recorded conformance scenarios](../../conformance/connectors/box/scenarios.json) define the provider request and retry behavior replayed by Python and TypeScript.
The [`file-update` Go Flow](go/examples/file-update/README.md) exercises
rename, move, and shared link creation against a mock through a real Dex Worker.
The [`folder-create` Go Flow](go/examples/folder-create/README.md) exercises
bounded folder creation through a real Dex Worker.

Provider references: [file metadata](https://developer.box.com/reference/get-files-id/),
[folder items](https://developer.box.com/reference/get-folders-id-items/),
[search](https://developer.box.com/reference/get-search/),
[file deletion](https://developer.box.com/reference/delete-files-id/), and
[folder deletion](https://developer.box.com/reference/delete-folders-id/).
Box [file update](https://developer.box.com/reference/put-files-id/) and
[folder update](https://developer.box.com/reference/put-folders-id/) document
the rename, move, and shared link request fields.
Box [direct file upload](https://developer.box.com/reference/post-files-content/)
documents the multipart attributes and file parts.
