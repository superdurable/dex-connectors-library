# Hacker News TypeScript connector

The public Hacker News API connector reads feed IDs and item details with Dex Query Steps. Install `@superdurable/connector-hackernews` and declare a `hacker-news` connection named `hackernews-public` in your Dex project. The checked-in [top-story example](examples/top-story/README.md) shows the typed Flow and Worker.

`HackerNewsClient` accepts `baseUrl`, `timeoutMs`, and `maxResponseBytes`; omitted values use the manifest defaults. `newProjectConnection` reads the named Dex project connection, including the manifest `timeout` in nanoseconds. The provider is public, so no credential is required. Provider calls occur only inside Step execution. `listStoryIDs` accepts top, best, new, ask, or show and a limit from 1 to 500. `getItem` reads one positive item ID and supplies its Hacker News discussion URL. Missing, deleted, and dead items select `notFound`; rate limits and availability failures retry.

Run `npm run check` for package tests, type checking, lint, and build. Conformance fixtures replay Go-recorded provider exchanges in both TypeScript and Python. The example integration needs a running `dexcli dev` stack; live Hacker News behavior has not been verified.
