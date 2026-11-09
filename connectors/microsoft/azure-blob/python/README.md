# Azure Blob Storage Python package

`Client.get_blob()` binds the signed, bounded `getBlob` Query. `GetBlobInput` contains `container` and `blob_name`; `BlobDownload` contains `data_base64`, `content_type`, and `etag`. See the [connector guide](../README.md) for Shared Key setup and the account limitation. The [runnable Flow](examples/blob_download/README.md) exercises a real Dex Worker with a local provider fixture. Live Azure access has not been verified.
