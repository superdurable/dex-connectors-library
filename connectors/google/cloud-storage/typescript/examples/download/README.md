# Object download Flow (TypeScript)

This runnable Flow downloads a bounded object body through the static `google-cloud-storage` connection. Configure OAuth as described in the [connector guide](../../../README.md). Set `GOOGLE_STORAGE_BUCKET` and `GOOGLE_STORAGE_OBJECT`, run `npm run example:download:worker`, then `npm run example:download:start`. The Flow stores Base64 content and its media type in a declared Attribute and completes with the same value. Download only content the application intends to persist in Dex.

With `dexcli dev` running, `npm run test:integration` exercises this Flow through a real Dex Worker and Client against the generated mock. Live Google authorization and API behavior have not been verified.
