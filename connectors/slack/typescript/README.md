# Slack TypeScript connector

Install `@superdurable/connector-slack@0.1.0` with `@superdurable/connectors-sdk@0.3.0` and `@superdurable/dex` 1.28 or newer. This package implements `listThreadMessages`, `getThreadReply`, `postChannelMessage`, and `postThreadReply`. Socket Mode Triggers remain Go-only.

Use `newProjectConnection(project, 'slack-thread-reader')` with a Dex project loaded by `loadFromEnvironment`. The project credential source refreshes the OAuth bot and user tokens using generated manifest declarations. Bot credentials send messages; user credentials read thread replies. The package does not place credentials in Flow state.

The runnable [thread page example](examples/list-thread-messages/README.md) is the minimal consuming application and shows the static connection name, Worker entrypoint, and Flow Definition Graph 2.0 source. The provider replay tests cover each operation against Go-recorded requests and decisions. Live Slack OAuth and provider behavior have not been verified for this package.
