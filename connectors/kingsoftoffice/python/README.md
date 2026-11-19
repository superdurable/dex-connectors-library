# WPS 365 connector for Python

Install `dex-connector-kingsoftoffice==0.1.0` after its first release. The package implements the `listFolderFiles` Query with `new_list_folder_files_step`. It returns one bounded page of file metadata without content, sharing links, or user data.

Create a Dex Web Connection named `kingsoftoffice`. At [WPS Open Platform](https://open.wps.cn), create an enterprise self-built application, request `kso.file.read`, publish it, and obtain administrator approval. Under 开发者后台 > 应用信息 copy APPID and APPKEY into the connection; APPKEY is secret. The connector exchanges them for an application token and optionally signs requests when 开发者后台 > 安全设置 > 接口签名 is enabled. Leave fixed endpoints and limits blank for the displayed defaults.

The happy branch is `listed`. `providerRejected`, `invalidResponse`, and `defect` are optional; unwired optional outcomes fail the example Flow. HTTP 408, 429, 5xx, and transport failures retry. The [runnable example](examples/folder_files/README.md) includes a Worker and a real Dex integration test with a fake provider. Live WPS authorization and responses remain unverified.

## Verification

```bash
uv sync --group dev
uv run pytest tests
uv run ruff check .
uv run mypy src
```
