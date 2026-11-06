# Salesforce record lookup example

This Flow reads one Contact through the static `salesforce-crm` Dex Web connection. Configure that connection using the Salesforce authorization guide, then run `npm run example:worker` and `npm run example:start -- 003RM000001AbcdYAC` from the TypeScript package. The Worker and start command are checked in under this directory.

The integration test uses a real `dexcli dev` server and an inert local Salesforce HTTP fake. Live Salesforce OAuth consent, JWT bearer authorization, and org API access remain unverified.
