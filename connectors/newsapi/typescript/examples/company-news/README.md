# Company news, TypeScript

`CompanyNewsFlow` accepts a company name, fixes a UTC search window in its
first Step, and calls NewsAPI's `searchArticles` Query once per Dex attempt.
The successful `searched` branch returns the newest article page. The optional
provider rejection, invalid response, and defect branches are unwired and fail
the Flow with the connector's safe Failure kind.

Run `npm ci` from `connectors/newsapi/typescript`. Configure the static
`newsapi` connection in the Dex project with a NewsAPI key in `api_key`, then
start `dexcli dev` and run `npm run example:company-news`. Start
`NewsAPICompanyNews` with input such as `{ "company": "Meridian", "days": 7,
"maxArticles": 20 }`. The Worker loads project configuration with
`loadFromEnvironment()` and resolves the key for each provider call.

For a credential-free Dex integration check, start `dexcli dev` and run
`npm run integration:company-news`. It starts the same Flow against a scripted
NewsAPI operation and checks success, a retried rate limit, and an unwired
authentication rejection with real Worker and Client state transitions.

The `SearchCompanyArticles` Step declares `groupId: newsapi`, group label
`NewsAPI`, and the explanation in [flow.ts](flow.ts). The package includes
generated `dex-connector.json`. To regenerate and check it from the repository
root, run:

```sh
GOWORK=off go run ./cmd/connectorctl generate connectors/newsapi/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate --check connectors/newsapi/connector.yaml
```

With Node.js 22 or 24 and the package dependencies installed, run strict FDG
2.0 visualization from this directory:

```sh
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/newsapi-company-news
```

Dex CLI 1.28.0 currently reports `dynamic_step_target` for the registered SDK
QueryStep instance and `unknown_step_target` for its incoming `goTo`, followed
by an `unreachable_step` warning. Strict FDG for this local-to-connector route
needs Dex CLI 1.28.1 (tracked in issue #362); the same Flow passes real Dex
Worker and Client tests.
Keep the named project connection outside Flow input until strict visualization
and Dex Web setup can be verified. Do not put the key in an Attribute or start
request.
