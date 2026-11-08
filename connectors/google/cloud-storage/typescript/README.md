# Google Cloud Storage connector for TypeScript

The TypeScript package implements `listObjects`, `downloadObject`, and `uploadObject` using the same [Google Cloud Storage connector manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for Google OAuth setup and operation behavior. The [object list](examples/objects/README.md), [download](examples/download/README.md), and [upload](examples/upload/README.md) examples use the static `google-cloud-storage` Dex Web connection.

Run `npm ci` and `npm run check` from this directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration` for real Dex Worker and Client coverage. Live Google authorization, Cloud Storage API calls, and Dex Web field rendering have not been verified in this checkout.
