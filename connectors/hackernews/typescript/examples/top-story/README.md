# Top story Flow

`worker.ts` runs the checked-in `HackerNewsTopStory` Flow with the static `hackernews-public` connection. Configure that public connection in Dex Web, then run `npm run example:top-story` with `DEX_FLOW_SERVICE_ADDRESS` set to your Dex Flow service. Start the Flow with `{ "feed": "top" }`; it returns the first item and its source and discussion links.

`integration.ts` runs the same Flow with generated provider mocks through a real Dex Worker and Client. Set `DEX_FLOW_SERVICE_ADDRESS` to a running `dexcli dev` service and run `npm run integration:top-story`.

From the repository root, generate and lint the Flow Definition Graph 2.0 source with:

```sh
dexcli visualize connectors/hackernews/typescript/examples/top-story/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/hackernews-top-story
```

Dex CLI 1.33.0 currently reports `dynamic_class` and `dynamic_step_target` for the generated Query Step factories and their target objects, so strict visualization needs a CLI analyzer fix. The same Flow runs through a real Dex Worker and Client. Preserve Step identities and the single `dex:explanation` beside each group directive.
