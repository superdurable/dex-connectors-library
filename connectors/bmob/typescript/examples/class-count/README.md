# Bmob class count example

`flow.ts` runs `countClassObjects` for a class name and persists only `{count}`. The static Dex Web connection is `bmob`. Configure the app-specific API origin, application ID, and REST API key using the connector's Dex Web guide. The Flow input contains only `className`.

Run `dexcli dev -open=false`, then `npm run example:worker` in one terminal and `npm run example:start -- GameScore` in another. For local FDG analysis before this package is published, run `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-bmob` once from the TypeScript package directory. Generate FDG 2.0 with `dexcli visualize --open=false --schema-version 2.0 examples/class-count/flow.ts`.

The integration test uses a local provider fake. Real Bmob account access and responses remain unverified.
