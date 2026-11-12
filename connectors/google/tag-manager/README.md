# Google Tag Manager connector

The Go, Python, and TypeScript connectors list one page of accounts, containers, or workspace tags through the [Tag Manager v2 API](https://developers.google.com/tag-platform/tag-manager/api/reference/rest). The [Go accounts Flow](go/examples/accounts/README.md), [Python accounts Flow](python/examples/accounts/README.md), and [TypeScript accounts Flow](typescript/examples/accounts/README.md) use the static `google-tag-manager` Dex Web connection and the operation-specific `listAccounts` Step.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable **Tag Manager API** under **APIs & Services → Library**. Configure OAuth consent, add `tagmanager.readonly` under **Data Access**, and add test users while an External app remains in Testing. Create a Web application under **Clients**, register the redirect URI displayed by Dex Web, then save its Client ID and Client Secret in the connection. Authorize a Google account with read access to the relevant Tag Manager account. Dex requests offline access for token renewal.

Call `listAccounts` to find an account path and numeric ID. Use that ID with `listContainers`, then find the workspace ID in the Tag Manager UI before calling `listTags`. Each list operation accepts an optional opaque page token and returns the next token; omit the token or leave it blank to read the first page. The API origin, timeout, and response limit have manifest defaults; override them only for a trusted gateway or known operational need. Tokens remain Dex secrets and never enter Flow input or results.

The [accounts.list](https://developers.google.com/tag-platform/tag-manager/api/reference/rest/v2/accounts/list), [containers.list](https://developers.google.com/tag-platform/tag-manager/api/reference/rest/v2/accounts.containers/list), and [tags.list](https://developers.google.com/tag-platform/tag-manager/api/reference/rest/v2/accounts.containers.workspaces.tags/list) methods define these read operations. Live Google authorization and API behavior have not been verified in this checkout.
