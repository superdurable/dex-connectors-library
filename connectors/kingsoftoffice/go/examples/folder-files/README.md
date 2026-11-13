# WPS 365 folder file listing Flow

This runnable Flow uses the static `kingsoftoffice` connection to list one page of file metadata. The `listed` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/kingsoftoffice/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/folder-files/flow/workflow.go --schema-version 2.0 --json --out ./build/folder-files
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override kingsoftoffice=<artifact-directory>`. In Dex Web **Connections**, create the `kingsoftoffice` connection. At [WPS 开放平台](https://open.wps.cn), finish enterprise certification, create an **企业自建应用**, request the application permission `kso.file.read` in **开发者后台 > 权限申请**, publish a version, and have an enterprise administrator approve it. Copy APPID and APPKEY from **开发者后台 > 应用信息**. Check **安全设置 > 接口签名** and set `requestSigningEnabled` accordingly. Leave the endpoint, timeout, response-size limit, and page size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/folder-files
```

In Dex Web **Start Flow**, choose `WPSListFolderFiles`, enter a unique Flow ID, and supply:

```json
{"driveId":"your_drive_id","parentId":"0","pageToken":""}
```

Use the drive ID returned by WPS's drive API or visible in the enterprise drive URL. `parentId` is the folder ID, with `0` for the root; a blank `pageToken` starts the first page. Pass `nextPageToken` into a later run to continue. Results expose only `items` and `nextPageToken`. The real Dex mock integration test uses synthetic file metadata and no WPS credentials.
