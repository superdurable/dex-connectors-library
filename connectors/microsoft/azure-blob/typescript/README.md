# Azure Blob Storage TypeScript package

`MicrosoftAzureBlobClient.getBlob()` binds the signed, bounded `getBlob` Query. `GetBlobInput` contains `container` and `blobName`; `BlobDownload` contains `dataBase64`, `contentType`, and `etag`. See the [connector guide](../README.md) for Shared Key setup and the account limitation. The [runnable Flow](examples/blob-download/README.md) exercises a real Dex Worker with a local provider fixture. Live Azure access has not been verified.
