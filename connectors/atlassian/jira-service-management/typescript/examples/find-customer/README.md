# Find a customer Flow

This example uses the static `jira-service-management-support` Dex Web connection. The Flow looks up one email address in a service desk, persists the bounded customer list, and completes with account IDs and display names. Configure the Atlassian OAuth app and connection as described in the [package README](../../README.md).

From `connectors/atlassian/jira-service-management/typescript`, run `npm ci` and `npm run example:find`. In another terminal run `npm run example:find:start -- examples/find-customer/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` to the Dex endpoint if it differs from the default. `npm run test:integration` verifies the same Flow through a real Dex Worker and Client against a local provider stand-in.
