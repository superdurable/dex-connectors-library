# Object upload Flow (TypeScript)

This runnable Flow conditionally uploads one small text object through the static `google-cloud-storage` connection. Configure OAuth as described in the [connector guide](../../../README.md). Set `GOOGLE_STORAGE_BUCKET`, run `npm run example:upload:worker`, then `npm run example:upload:start`. The start command chooses a unique example object name and Base64 encodes `hello`; the Flow stores the returned name and generation in its declared Attribute.

The upload creates an object only when its name is absent. A 412 response triggers an immutable-generation content comparison. With `dexcli dev` running, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture. Live Google authorization and API behavior have not been verified.
