# Object list Flow (TypeScript)

This runnable Flow lists one Cloud Storage bucket page through the static `google-cloud-storage` connection. Configure OAuth as described in the [connector guide](../../../README.md). Set `GOOGLE_STORAGE_BUCKET` to a bucket name and run `npm run example:worker` in one terminal, then `npm run example:start` in another. The Flow stores the returned page in its declared Attribute and completes with the same page.

With `dexcli dev` running, `npm run test:integration` exercises this Flow through a real Dex Worker and Client against a local provider fixture. Live Google authorization and API behavior have not been verified.
