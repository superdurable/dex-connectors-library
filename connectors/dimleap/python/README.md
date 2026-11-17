# Dimleap Python connector

`dex-connector-dimleap` implements the read-only `listModels` Query for Dex Flows. It calls the documented `GET /v1/models` endpoint with a Bearer API Key and returns the current total count and at most the first 50 model IDs. Availability changes with the Key and provider catalog; no model list is embedded in this package.

Create the static `dimleap` connection in Dex Web. At [Dimleap](https://www.dimleap.cn), sign in, open **Console > API key management**, create an API Key, and copy it once to secret `apiKey`. Blank prevents requests. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults; timeout is measured in seconds in Python and response size in bytes. The fixed production endpoint is `https://api.dimleap.cn/v1/models` and has no user-entered base URL. The Query has no operation-specific configuration fields.

The connector returns only model IDs and count, never the Key or other metadata. HTTP 429/5xx and transport failures retry. Definite authorization or other HTTP rejections select optional `providerRejected`; malformed or oversized data selects `invalidResponse`, and missing local credentials select `defect`. An unwired optional branch fails the Flow.

The [runnable model catalog Flow](examples/model_catalog/README.md) records the returned IDs in a Dex Attribute. Its integration test uses inert credentials and a fake provider against a real Dex Worker and Client. Live Dimleap Key authorization and current catalog contents remain unverified without a user-controlled Key.
