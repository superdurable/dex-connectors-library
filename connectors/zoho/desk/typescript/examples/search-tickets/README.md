# Zoho Desk TypeScript search

Run `dexcli dev -dex-port 40872 -web-port 40873 -open=false`, configure the OAuth connection named `zoho-desk-helpdesk` in Dex Web, and set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40872`. Start the Worker with `npx tsx examples/search-tickets/worker.ts`, then start the sample Flow with `npx tsx examples/search-tickets/start.ts examples/search-tickets/input.example.json` from `typescript/`.

The integration test uses the generated mock through a real Dex Worker and Client. Live Zoho authorization and provider delivery remain unverified.
