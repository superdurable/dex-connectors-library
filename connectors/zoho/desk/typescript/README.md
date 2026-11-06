# Zoho Desk TypeScript connector

Dex-native Zoho Desk searches, ticket reads, ticket creation, updates, and comments. The package uses generated OAuth refresh wiring for all nine Zoho data centers and pins `@superdurable/connectors-sdk` 0.7.0.

The [runnable search Flow](examples/search-tickets/README.md) uses the static `zoho-desk-helpdesk` connection. Its integration test runs the generated mock through a real Dex Worker and Client. Provider replay compares requests and outcomes with Go recordings; live Zoho authorization and provider behavior remain unverified.
