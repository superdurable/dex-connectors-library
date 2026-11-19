# Microsoft Teams TypeScript connector

`@superdurable/connector-microsoft-teams@0.1.1` provides four Microsoft Teams operations for Dex Flows. Create a named Microsoft Teams OAuth connection in Dex Web before running the [thread-replies example](examples/list-thread-replies/README.md). Live Microsoft authorization and provider calls remain unverified.

Posts follow the Go connector's duplicate-safe posting rules: an unknown outcome is read back when possible and resent only while possible sends stay below `maxSends` (default 3), and `uncertain` is selected only at that cap or when several matching messages are found.

A failed read of a non-2xx Graph error body retains the HTTP status, so 429 remains retryable and a 404 read remains `notFound`. Thread cursors compare decoded paths as Go does.
