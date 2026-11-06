# Entra user lookup Flow

The static Dex Web connection name is `entra-directory`. Configure the Microsoft Entra ID connector in Dex Web Connections with an app-only registration or administrator OAuth. The connector [setup guide](../../../README.md) lists the required Microsoft Graph permissions and roles. The Flow reads a user by object ID or principal name and completes with the bounded account record. A missing account selects the optional `notFound` branch and fails the Flow because this example wires only `found`.

From this package directory, start Dex with a free local port pair, then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40630 npm run example:worker
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40630 npm run example:start -- ada@contoso.com
```

For an inert provider verification with no Microsoft credentials, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40630 npm run test:integration`. This test starts the same checked-in Flow through a real Dex Worker and Client.
