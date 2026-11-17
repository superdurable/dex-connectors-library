# Story search Flow

`flow.ts` defines the `HackerNewsStorySearch` Flow with the static `hackernews-public` connection. It validates the request, fixes the search window when it starts, reads pages with the generated `newSearchStoriesStep`, keeps each story once (a newest-first page can repeat one when newer stories arrive), and completes with the report. Start it with `{ "query": "postgres" }`; optional `withinHours` (1 to 720, default 24), `tags`, `minPoints`, `sortBy` (`date` by default, or `relevance`), and `maxPages` (1 to 20 pages of 50 stories, default 4) narrow the search. A blank query or an out-of-range window or page budget fails the Flow before any provider request; other invalid input selects the connector's unwired `defect` branch, which fails the Flow.

`worker.ts` runs the Flow with the project connection: configure `hackernews-public` in Dex Web with no credentials, set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local `connections.json`, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run example:story-search`.

`integration.ts` runs the same Flow with generated mocks through a real Dex Worker and Client: three scripted pages with a repeated story, a page budget, a retried rate limit, and an invalid request. Set `DEX_FLOW_SERVICE_ADDRESS` to a running `dexcli dev` service and run `npm run integration:story-search`.

From the repository root, generate and lint the Flow Definition Graph 2.0 source with:

```sh
dexcli visualize connectors/hackernews/typescript/examples/story-search/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/hackernews-story-search
```

As for the top-story example, Dex CLI 1.47.3 reports `dynamic_class`, `dynamic_step_target`, and `unknown_step_target` for the generated Query Step factory and its targets, so strict visualization, and with it Dex Web connection setup and Start Flow for this Flow, needs a CLI analyzer fix. The Flow runs through a real Dex Worker and Client.
