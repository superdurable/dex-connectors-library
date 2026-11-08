# Adobe Acrobat Sign connector

The Go `getAgreement` Query reads an agreement's ID, name, and status from Acrobat Sign REST API v6. It excludes participant identities and document contents from the Dex result. Python and TypeScript packages are not yet implemented.

## Connection

In an Acrobat Sign Enterprise account, open **Acrobat Sign API > API Information > Integration Key** and create an integration key with the `agreement_read` scope. If Integration Key is unavailable, request enablement from Acrobat Sign support. Open **Personal Preferences > Access Tokens**, select the key, reveal its Integration Key value, and store it in the Dex Web connection's secret `integration_key` field. Do not enter the key in a Flow input. See Adobe's [integration key setup](https://helpx.adobe.com/sg/sign/web/integrations/integration-keys/create.html) and [key retrieval](https://helpx.adobe.com/my_en/sign/web/integrations/integration-keys/retrieve.html).

Leave `baseUrl` blank for the displayed commercial discovery endpoint. The connector calls `baseUris` to resolve the account's regional `apiAccessPoint`, validates it, and caches it for that integration key before reading the agreement. `timeout` is a per-request Go duration; `maxResponseBytes` is a byte limit. Leave both blank for their displayed defaults. The query input `agreementId` is the non-secret ID returned by Acrobat Sign when an agreement is created. See Adobe's [API getting started](https://experienceleague.adobe.com/en/docs/acrobat-services-learn/tutorials/acrobatsign/signapi) and [agreement API](https://developer.adobe.com/acrobat-sign/docs/overview/developer_guide/apiusage).

## Example and verification

Run the [agreement inspection Flow](go/examples/agreement-inspection/README.md) with a static `adobe-acrobat-sign` connection. Provider tests and the mock-backed Dex integration test cover the checked-in path. A real Adobe authorization and agreement read have not been verified locally.
