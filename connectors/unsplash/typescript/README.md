# Unsplash TypeScript connector

Install `@superdurable/connector-unsplash` and `@superdurable/dex`. The package exposes generated `searchPhotos` and `getPhoto` Query Step factories, a typed `Client`, and `newProjectConnection` for Dex Web connection settings. The package version is `0.1.0`; the Go connector release has its own version.

The checked-in [photo search example](examples/photo-search) binds the static `unsplash` connection. From a configured Dex project, run `npm run example:photo-search` and start `UnsplashTypeScriptPhotoSearch` with `{"query":"office","perPage":5}`. The Flow stores a public photo page with hotlink and attribution URLs. Leave optional branches unwired unless the application has a specific reaction; an unwired branch fails the Flow.

For local verification, start `dexcli dev -dex-port 30201 -web-port 30202 -open=false` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:30201 npm run integration:photo-search`. The integration script binds a provider stand-in on a free port and checks retry, success, and an unwired rejection through a real Dex Worker and Client. Stop `dexcli dev` afterward.

Create an Unsplash application at [Your apps](https://unsplash.com/oauth/applications) and copy its Access Key into the Dex Web Connections secret `access_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their manifest defaults. `timeout` is a Go duration such as `15s`; override it for a slow network. The Access Key is sent only in `Authorization: Client-ID`.

Search requires a nonblank `query`. `page` is one based; zero omits it. `perPage` accepts 0 through 30, and zero uses the provider default. Photo lookup accepts an ID without `/`, `?`, `&`, or `#`. Valid replies select `searched` or `found`; a missing photo selects `notFound`; definite rejections, invalid responses, and local defects select their named branches. Transport errors, 408, 429, and 5xx return Retry. The result includes Unsplash hosted image and photographer URLs; hotlink images and attribute the photographer and Unsplash.

Go recorded provider scenarios are in `conformance/connectors/unsplash/scenarios.json`, replayed by both language packages. Live Unsplash authorization and provider responses have not been verified.
