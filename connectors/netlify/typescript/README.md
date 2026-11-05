# Netlify connector for TypeScript

`@superdurable/connector-netlify` provides typed Dex Query Steps for Netlify's site and deploy lists. It implements `listSites` and `listDeploys` with the same branch identities as the Go connector. Both return the first provider page.

## Use

The [runnable site lookup](examples/netlify-lookup/README.md) binds a static Dex Web connection named `netlify`. Configure that connection with a Netlify personal access token from [User settings > Applications > Personal access tokens](https://app.netlify.com/user/applications#personal-access-tokens). The token is resolved for each provider call and stays out of Flow inputs and Attributes.

Application code imports `NetlifyClient`, `newConnection`, `newListSitesStep`, and `newListDeploysStep` from this package. The example shows the complete Worker, Flow, and starter. The Step factories route `listed` by default; optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches may be wired for a business response. An unwired optional outcome fails the Flow.

The provider client accepts manifest configuration values `baseUrl`, `timeoutMs`, and `maxResponseBytes`. Omit them for the manifest defaults. `timeoutMs` is milliseconds and `maxResponseBytes` is bytes. Override `baseUrl` only for a compatible HTTPS gateway or a loopback test server.

## Verification

Run `npm run check` for build, unit tests, type checking, and lint. The conformance test replays the Go-recorded provider requests and decisions. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<port> npm run test:integration` with `dexcli dev` serving that port to execute the checked-in Flow through a real Dex Worker and Client. The integration test starts a local provider stand-in on a free port.

Live Netlify authorization and provider calls have not been verified.
