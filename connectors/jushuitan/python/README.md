# Jushuitan Python connector

`dex-connector-jushuitan` implements the `listShops` Query. Configure App Key, App Secret, and Access Token from [Jushuitan Open Platform](https://openweb.jushuitan.com/user/login) in Dex Web Connections. The Query returns only enabled shop IDs, names, sites, and page metadata.

See the [runnable shop-list Flow](examples/shops/) for Worker and connection setup. Go-recorded conformance scenarios cover signed request and decision parity. The checked-in Dex integration test uses a fake provider; real authorization and shop retrieval have not been verified.
