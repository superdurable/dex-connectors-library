# PandaDoc connector

The `getDocument` Query calls PandaDoc's [Document Status endpoint](https://developers.pandadoc.com/reference/document-status) at `GET /documents/{id}` and returns only the document ID, name, and status. Recipients, fields, tokens, and pricing are excluded. Missing documents select `notFound`; rate limits, transport failures, and 5xx responses retry.

PandaDoc [API key authentication](https://developers.pandadoc.com/reference/api-key-authentication-process) uses `Authorization: API-Key <key>`. In the PandaDoc [Dev Center Configuration page](https://app.pandadoc.com/a/#/api-dashboard/configuration), create a Sandbox key or obtain an approved Production key. Save the complete opaque value in Dex Web's secret `api_key` field. The key is resolved for each call and never belongs in Flow input. The default API root is global; accounts signed in at `app.pandadoc.eu` use `https://api.pandadoc.eu/public/v1` as `baseUrl`, per PandaDoc's [API version reference](https://developers.pandadoc.com/reference/version).

The runnable Go Dex Flow is in [`go/examples/document-inspection`](go/examples/document-inspection/README.md). Its static connection name is `pandadoc`. Go, Python, and TypeScript packages each declare their first `v0.1.0` release and share Go-recorded conformance scenarios.
