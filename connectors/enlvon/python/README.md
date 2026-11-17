# Enlvon Python connector

`dex-connector-enlvon` implements the read-only `searchPolicies` Query for Dex Flows. It follows [Enlvon's documented `GET /api/search`](https://enlvon.com/docs), sending natural-language `q`, optional `top_k`, and an `enl_` key in `X-API-Key`. The result contains only title, date, source identifier, and original government URL. Detailed analysis, full document text, and credentials are discarded.

## Connection

Create a static `enlvon` connection in Dex Web. Read the [Enlvon API guide](https://enlvon.com/docs), then use **申请内测** at [the application page](https://enlvon.com/apply) to request a dedicated programmatic `enl_` key. Enter it in secret `api_key`; blank prevents authenticated search. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for displayed defaults. The fixed production origin is `https://enlvon.com`; Python timeout is in seconds and response limit in bytes.

Supply a nonempty `query` of at most 1,024 UTF-8 bytes. Optional `topK` is 1–50; zero omits the parameter and uses the provider's current default. The provider documents 401 for invalid keys and 429 for exhausted quota. The connector has a three-attempt, three-minute retry budget for interactive searches that consume provider quota. Definite provider rejections select optional `providerRejected`, malformed or oversized responses select `invalidResponse`, and invalid local input selects `defect`.

The [runnable policy directory Flow](examples/policy_directory/README.md) persists only the restricted card metadata in a Dex Attribute. Its integration test uses an inert key and fake provider with a real Dex Worker and Client. Live Enlvon authorization, current corpus contents, and account-specific quota remain unverified without a user-controlled key.
