# GitHub repository releases Flow

This Flow reads one page of a repository's releases and completes with its bounded `ReleasePage`. It uses the static Dex Web connection name `github-repository-releases`. Its only wired branch is `listed`; an optional failure branch fails the Flow.

1. Configure a GitHub OAuth connection named `github-repository-releases` in Dex Web. Start at [GitHub Developer settings](https://github.com/settings/developers), open **OAuth Apps**, register the exact Dex redirect URI, and provide the app client ID and secret. Dex requests `read:user`, `user:email`, and `offline_access` during consent.
2. Start a Dex development server and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
3. In this package, run `npm ci`, then `npm run example:list` for the Worker. In another terminal run `npm run example:list:start -- examples/repository-releases/input.example.json`.

The application input contains `owner`, `repository`, and optional one-based `page` and `pageSize`. The example JSON is safe to edit. The Worker uses the project credential provider, so no token belongs in the input or source file.

`npm run test:integration` runs the same Flow through a real Dex Worker and Client with a local GitHub stand-in. Live GitHub OAuth authorization and API behavior require a user-controlled connection and have not been verified by that test.
