# WordPress TypeScript provider operations

`PostCreationFlow` creates a draft or published post. `PostLookupFlow` reads one published post, and `PostListFlow` reads a page. All use the static Dex Web connection name `wordpress`.

Install dependencies with `npm ci`, configure the WordPress connection in Dex Web, and run `npm run example:worker` with `DEX_FLOW_SERVICE_ADDRESS` set. Start each Flow with its matching generated input type. The application password remains in Dex credentials.

Generate strict FDG 2.0 graphs from this package directory:

```bash
dexcli visualize examples/provider-operations/post-creation-flow.ts --schema-version 2.0 --json --out /tmp/wordpress-typescript-create-fdg --lint app
dexcli visualize examples/provider-operations/post-lookup-flow.ts --schema-version 2.0 --json --out /tmp/wordpress-typescript-lookup-fdg --lint app
dexcli visualize examples/provider-operations/post-list-flow.ts --schema-version 2.0 --json --out /tmp/wordpress-typescript-list-fdg --lint app
```

The integration test runs these Flows through a real `dexcli dev` server and manifest mocks. It does not contact WordPress. Live provider authorization remains unverified until a user-owned connection is available.
