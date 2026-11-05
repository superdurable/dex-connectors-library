# Vercel deployment lookup

Create a Dex Web connection named `vercel`, using a Vercel access token from [Account Settings > Tokens](https://vercel.com/account/tokens). From this package directory, run `npm ci` and `npm run build`.

Start a local Dex stack with `dexcli dev`, then run `npm run example:lookup` for the Worker. In another terminal, run `npm run example:lookup:start -- examples/vercel-lookup/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a nondefault gRPC port. The input contains a deployment ID or hostname and may include `teamId`.

The Flow persists the nonsecret lookup input and deployment result. A provider rejection, missing deployment, malformed response, or local defect selects an unwired optional branch and fails the Flow.
