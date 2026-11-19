# PandaDoc connector

The Go connector supports these document operations through the [PandaDoc Public API](https://developers.pandadoc.com/reference/about):

| Operation | Kind | Repeat tier |
| --- | --- | --- |
| `getDocument` | Query | safe |
| `listDocuments` | Query | safe |
| `updateDocument` | Mutation | safe |
| `deleteDocument` | Mutation | safe |
| `createDocument` | Mutation | limited, 3 sends |
| `sendDocument` | Mutation | limited, 3 sends |

`getDocument` returns only the ID, name, and status. `listDocuments` searches by name or reference number with bounded pagination. `updateDocument` replaces a draft's name and `deleteDocument` treats an already absent document as deleted. Creation requires a template and role-assigned recipients; it writes a non-secret Dex call marker into document metadata and searches that marker before a possible resend. Sending reads document status before a possible resend. A send can duplicate recipient notifications, so its unconfirmed outcome becomes `uncertain` after three possible sends.

PandaDoc [API key authentication](https://developers.pandadoc.com/reference/api-key-authentication-process) uses `Authorization: API-Key <key>`. In the PandaDoc [Dev Center Configuration page](https://app.pandadoc.com/a/#/api-dashboard/configuration), create a Sandbox key or obtain an approved Production key. Save the complete opaque value in Dex Web's secret `api_key` field. The key is resolved for each call and never belongs in Flow input. The default API root is global; accounts signed in at `app.pandadoc.eu` use `https://api.pandadoc.eu/public/v1` as `baseUrl`, per PandaDoc's [API version reference](https://developers.pandadoc.com/reference/version).

The runnable Go Dex Flows are in [`go/examples/document-inspection`](go/examples/document-inspection/README.md). Its static connection name is `pandadoc`. The Go release is `v0.2.0` and requires Connector SDK `v0.38.0`; Python and TypeScript remain at `v0.1.0` with only `getDocument`. Go-recorded conformance scenarios are filtered to each language's declared operations.
