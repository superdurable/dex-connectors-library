# ApiZero TypeScript connector

`@superdurable/connector-apizero` provides the read-only `getAccountInfo` Dex Step. It reads the current Key's wallet balance or prepaid quota through [ApiZero's account-information API](https://apizero.cn/aidocs/user-info/raw.md).

Use the [runnable Flow and Worker](examples/account-info/) with the static `apizero` connection. In Dex Web Connections, sign in at [API Key](https://apizero.cn/account/keys), create or copy an `sk_live_` Key, and save it in the protected field. The Key is resolved for each call and never enters Flow state. The endpoint is fixed; timeout and response size have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real ApiZero authorization, wallet balance, and prepaid quota remain unverified without a user-controlled Key.
