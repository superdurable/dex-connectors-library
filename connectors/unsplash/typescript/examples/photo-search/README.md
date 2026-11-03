# Photo search example

`main.ts` starts a Worker for `UnsplashTypeScriptPhotoSearch` with the static `unsplash` Dex Web connection. Configure an Unsplash Access Key in Dex Web Connections, run `npm run example:photo-search`, and start the Flow with `{"query":"office","perPage":5}`. Its output is a page of public hotlink and attribution URLs.

`integration.ts` runs the same Flow against a provider stand-in and a real Dex Worker and Client. Start `dexcli dev` on free ports in 30200–30299, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, then run `npm run integration:photo-search`. The stand-in binds an OS assigned free port. This verifies Dex retry and branch routing; live Unsplash behavior remains unverified.
