# Google Cloud Storage connector

The Go connector lists one page of object identities and downloads a bounded object body through the [Cloud Storage JSON API](https://docs.cloud.google.com/storage/docs/json_api/v1). The runnable [object-list Flow](go/examples/objects/README.md) uses the static `google-cloud-storage` Dex Web connection. Python, TypeScript, uploads, and object-created delivery are still to be implemented.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable Cloud Storage JSON API in **APIs & Services → Library**. Configure Google Auth Platform consent, audience, and test users as needed. Add the displayed `devstorage.read_write` scope, create a Web application under **Clients**, register the exact redirect URI shown in Dex Web, and save its Client ID and Client Secret there. Authorize a Google account with bucket access and offline access. Tokens remain Dex secrets.

Find the bucket name under **Cloud Storage → Buckets**. `listObjects` accepts the bucket name, optional object name prefix, page size from 1 through 1000 (default 100), and a page token from a previous response. It returns object names, immutable generation strings, and a next page token. Pass a returned name and optional generation to `downloadObject`; it returns content as Base64 plus the HTTP content type. Download size is limited by `maxResponseBytes` (default 2 MiB). The API origin and request timeout have manifest defaults. Object content is placed in a Flow result, so applications should only download content they intend to persist in Dex.

[Objects: list](https://docs.cloud.google.com/storage/docs/json_api/v1/objects/list) and [download objects](https://docs.cloud.google.com/storage/docs/downloading-objects) define the two requests. Live Google OAuth authorization, Cloud Storage reads, and Dex Web field rendering have not been verified in this checkout.
