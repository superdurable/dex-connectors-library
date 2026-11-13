# ZenTao

The Go connector lists products visible to a ZenTao REST v1 account. `ListProducts` calls [GET `/api.php/v1/products`](https://www.zentao.net/book/api/675.html) and returns product IDs, names, codes, types, access controls, and the reported total. It omits owners, creators, and whitelist members from the provider response. The official v1 endpoint does not document pagination, so this operation retrieves its single response and rejects more than 1,000 products or an oversized response.

This connector requires ZenTao 16.5 or later. In Dex Web, enter the HTTPS root of your own installation, including `/zentao` if your login URL contains that subpath. A self-hosted instance has no universal endpoint. Obtain the token in a trusted client by POSTing JSON `account` and `password` to `<installation-root>/api.php/v1/tokens`, then copy only the returned `token` into the secret connection field. The connector never stores the account password or returns the token in Flow data or errors. See the [official setup guide](https://www.zentao.net/book/api/1397.html) and [token endpoint](https://www.zentao.net/book/api/664.html).

The account needs permission to view products. The [runnable Dex Flow](go/examples/product-list/README.md) shows the local mock path. Live ZenTao instance authorization and provider response remain unverified.

The logo uses the official [ZenTao website footer asset](https://static.zentao.net/web/data/source/zentao/default/wide/logo-footer.png) embedded unchanged in SVG.
