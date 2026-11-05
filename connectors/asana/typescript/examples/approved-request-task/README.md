# Approved request task example

The [Flow](flow.ts) uses all five Asana operations. It lists a project's tasks, reuses a task with the same name or creates one, reads it, sets `isCompleted` to true, then adds the supplied plain-text approval comment. It persists only nonsecret request and result data. The static Dex Web connection name is `asana-approved-requests`.

Create an Asana personal access token at <https://app.asana.com/0/my-apps> under **Personal access tokens**, then save it in Dex Web **Connections** for the Asana connector with that name. The project GID is the decimal number in the project URL. `endpoint` defaults to the public Asana API; a loopback URL is only for local tests. `maxResponseBytes` defaults to the manifest safety limit. The connector's authorization guide in Dex Web owns the full setup instructions.

From the repository root, install and build with `npm ci --prefix connectors/asana/typescript` and `npm run build --prefix connectors/asana/typescript`. Start a Dex development server, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and set the Dex project environment generated for the configured connection. Run `npm run example:approved-request-task --prefix connectors/asana/typescript`. The [Worker entrypoint](main.ts) runs until interrupted; start a Flow of type `AsanaApprovedRequest` with JSON input containing `projectId`, `name`, and `comment` strings.

For a real Dex integration with a local Asana stand-in, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration --prefix connectors/asana/typescript`. The integration test exercises a 429 retry, every operation, and an unwired `providerRejected` branch. Live Asana behavior remains unverified.
