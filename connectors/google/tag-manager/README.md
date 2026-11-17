# Google Tag Manager connector

The Go, Python, and TypeScript connectors list one page of accounts, containers, or workspace tags through the [Tag Manager v2 API](https://developers.google.com/tag-platform/tag-manager/api/reference/rest). Go also reads account, container, workspace, tag, trigger, variable, folder, template, environment, and version identities. The [Go accounts Flow](go/examples/accounts/README.md), [Python accounts Flow](python/examples/accounts/README.md), and [TypeScript accounts Flow](typescript/examples/accounts/README.md) use the static `google-tag-manager` Dex Web connection.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable **Tag Manager API** under **APIs & Services → Library**. Configure OAuth consent, add `tagmanager.readonly` under **Data Access**, and add test users while an External app remains in Testing. Create a Web application under **Clients**, register the redirect URI displayed by Dex Web, then save its Client ID and Client Secret in the connection. Authorize a Google account with read access to the relevant Tag Manager account. Dex requests offline access for token renewal.

Call `listAccounts` to find an account path and numeric ID. Use that ID with `listContainers`, then call Go's `listWorkspaces` with the returned container path. The existing `listTags` operation accepts numeric account, container, and workspace IDs. Go's `getAccount`, `getContainer`, `getWorkspace`, `getTag`, `getTrigger`, `getVariable`, `getFolder`, `getTemplate`, `getEnvironment`, and `getVersion` accept the matching API relative path. Go's `listTriggers`, `listVariables`, `listFolders`, `listTemplates`, and `listEnvironments` accept the workspace or container path as `parentPath`. Every list accepts an optional opaque page token; blank starts with the first page. These Go operations return only path and display name, never configuration fields that might contain secrets.

The API origin, timeout, and response limit have manifest defaults; override them only for a trusted gateway or known operational need. Tokens remain Dex secrets and never enter Flow input or results. The OAuth grant requests `tagmanager.readonly`, so writes require a separate connection type and are unavailable here.

The [Tag Manager v2 reference](https://developers.google.com/tag-platform/tag-manager/api/reference/rest) defines these read operations. Live Google authorization and API behavior have not been verified in this checkout.
