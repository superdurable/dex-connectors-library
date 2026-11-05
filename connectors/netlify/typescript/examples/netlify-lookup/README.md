# Netlify site lookup example

This Flow lists the first page of Netlify sites with the static Dex Web connection name `netlify`. Configure that connection with a personal access token through Dex Web Connections. The Worker resolves the token per request and stores only the site page.

Start a Dex stack, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run example:lookup` from the TypeScript package. In another terminal, run `npm run example:lookup:start -- examples/netlify-lookup/input.example.json`. The input is an empty JSON object because `listSites` takes no fields.

The integration test runs this same Flow against a real `dexcli dev` service with a local provider stand-in. Live Netlify authorization and provider behavior remain unverified.
