# Pinecone TypeScript sample

This Flow upserts one bounded vector batch with the static `pinecone` connection.
Create the connection in Dex Web using the Pinecone index host and an `api_key`
from your project. The key is held by Dex and is never a Flow input.

From `connectors/pinecone/typescript`, run `npm ci` and `npm run build`, then
start `dexcli dev` in another terminal. Set `DEX_FLOW_SERVICE_ADDRESS` to its
gRPC address and load the Dex project configuration in the environment. Run
`npm run example:worker` to register the Worker, then
`npm run example:start -- examples/sample-pinecone/input.example.json` to start
one Flow. The JSON input can be changed to a vector with your index dimension.

The local integration test uses this same Flow with a temporary HTTP fixture
and a real Dex Worker and Client. It checks success and an unwired rejection.
