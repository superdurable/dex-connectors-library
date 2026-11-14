# TokenGP connector

[TokenGP](https://www.tokengp.com/about) is operated by 深圳市星驰通讯器材有限公司. This Go connector calls the official [`GET /v1/models`](https://www.tokengp.com/docs) API to list model IDs accessible to the connected API key. It does not invoke a model, bill for generation, or return model pricing, key details, or account information.

## Connection

Sign in to [TokenGP Keys](https://www.tokengp.com/keys), choose **Create Key**, and copy the `sk-tokengp-` secret into the Dex `api_key` credential. The key's model and IP restrictions determine access. Dex sends it only in an Authorization Bearer header. Leave base URL, timeout, and response limit blank for displayed defaults. Revoke a compromised key on the Keys page and create a replacement.

## Go

The [model catalog Flow](go/examples/model-catalog/) is a runnable Worker with static Dex Web connection name `tokengp`. `listed` completes the Flow; optional branches fail unless an application wires a business response. Transient failures retry safely. The local mock integration uses synthetic model data and a real Dex Worker and Client. Live authorization and accessible model IDs remain unverified without a user-controlled connection.
