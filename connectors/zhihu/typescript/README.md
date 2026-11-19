# Zhihu TypeScript connector

Dex Query `listQuotas` reads Zhihu provider data. [`examples/basic`](examples/basic) contains a runnable Worker and Client. The package implements the operation and no Triggers, matching its manifest. Nine Go-recorded provider scenarios are replayed by `npm test`. Run `npm run test:integration` with a local Dex service to verify retry and persistence. Live provider authorization remains unverified.
