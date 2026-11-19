# Bmob TypeScript connector

`@superdurable/connector-bmob` provides `countClassObjects`, a read-only Query that returns the count for one Bmob class without reading records. The host resolves the application ID and REST API key for each call; they never enter Flow input or persisted output.

The runnable [class count example](examples/class-count/README.md) binds the static `bmob` connection. Configure the app-specific API origin and credentials in Dex Web Connections using `connector.yaml` guidance. The package replays Go provider scenarios for successful counts, rejections, malformed responses, bounded reads, rate limits, and transport failures. Its integration test exercises a real Dex Worker and Client against a local provider fake. A real Bmob application and authorization have not been verified.

Install with `npm install @superdurable/connector-bmob`. `BmobClient` validates its configured API origin and request limits during construction. `newProjectConnection` loads the named project connection. Normal Flows use the generated `newCountClassObjectsStep` factory; `BmobClientOptions.send` supports an approved transport or deterministic test sender.
