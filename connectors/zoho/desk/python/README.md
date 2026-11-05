# Zoho Desk Python connector

Dex-native Zoho Desk searches, ticket reads, ticket creation, updates, and comments. The package uses the generated OAuth refresh declaration for all nine Zoho data centers and requires `dex-connectors-sdk==0.7.0`.

The runnable [search example](examples/search_tickets/README.md) uses the static `zoho-desk-helpdesk` connection. Its integration test runs the generated mock through a real Dex Worker and Client. Provider replay compares requests and outcomes with Go recordings; live Zoho authorization and provider behavior remain unverified.
