# Microsoft Entra ID connector for TypeScript

This package implements the eight Microsoft Graph operations in the connector manifest. It uses `@superdurable/connectors-sdk@0.7.0` for named connections, operation Steps, credential renewal, and response classification. App-only client credentials and administrator OAuth both use the generated manifest grant declaration; the package does not implement a separate token exchange.

Use `newProjectConnection(project, 'entra-directory')` after `loadFromEnvironment()` to bind the connection configured in Dex Web. The runnable [user lookup example](examples/user-lookup/README.md) uses `newGetUserStep` in a real Dex Flow. The other generated operation-specific factories are `newListUsersStep`, `newCreateUserStep`, `newDisableUserStep`, `newEnableUserStep`, `newRevokeSignInSessionsStep`, `newAddUserToGroupStep`, and `newRemoveUserFromGroupStep`.

```bash
npm ci
npm run check
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40630 npm run test:integration
```

The Go-recorded [conformance fixture](../../../../conformance/connectors/microsoft/entra-id/scenarios.json) verifies every branch, retry, transport path, 401 renewal, creation-key convergence, and group membership read-back through the production SDK operation path. The integration test exercises a Dex Worker and Client with an inert Graph stand-in. Live Microsoft authorization, admin consent, and tenant API behavior remain unverified.
