# Enlvon policy search connector

This connector searches the Chinese electricity policy corpus through [Enlvon's official API](https://enlvon.com/docs). It returns only policy title, publication date, source identifier, and original document link. The output omits full text, generated analysis, and provider credentials. Enlvon is operated by 鲸筑（上海）科技发展有限公司, as the official documentation footer states.

## Connection

Request a dedicated programmatic `enl_` API key through **申请内测** on [Enlvon's API guide](https://enlvon.com/docs), then enter it into the Dex Web `enlvon` connection. The connector sends it in `X-API-Key`. The provider also offers limited anonymous requests, but this connection uses a key for accountable programmatic access. Leave other connection fields blank for their documented defaults.

`searchPolicies` accepts a nonempty `query` and optional `topK` from 1 to 50 (zero uses the provider default of 10). The provider documents 401 for invalid keys and 429 for exhausted quota. The short retry budget serves an interactive user and prevents repeated requests from consuming the limited search quota indefinitely. A query may itself count against the provider's quota.

The [runnable Dex Flow](go/examples/policy-directory/README.md) uses the static `enlvon` connection and contains a synthetic mock. Live provider authorization and account-specific quota behavior require a user-controlled Enlvon key and have not been verified here.

## Python

The [Python package](python/README.md) implements `searchPolicies` and includes a [runnable Dex Flow](python/examples/policy_directory/README.md). Its 18 Go-recorded scenarios cover result filtering, default `topK`, provider branches, quota Retry, and local validation. No live Enlvon key has been authorized for this port.
