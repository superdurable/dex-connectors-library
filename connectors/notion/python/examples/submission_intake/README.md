# Notion submission intake example

This Flow searches Notion titles, queries a data source, creates a page with
a fenced Mutation, replaces a page's properties with an absolute update, and
reads the final page. Each provider operation is a Dex Step. The application
wires only the happy-path branch of each Step; an unwired optional outcome
fails the Flow. The static connection name is `notion-submissions`.

Generate the connector's strict FDG 2.0 setup reference from its checked-in Go
Flow until Dex CLI supports Python local-to-connector routing analysis:

```sh
cd connectors/notion/go/examples/submission-intake
mkdir -p build
dexcli visualize ./flow/workflow.go --schema-version 2.0 --json \
  --out ./build/notion-submission-intake
```

The output `build/notion-submission-intake.json` must contain `"valid": true`.

1. Start a local Dex stack with `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS`
   to its Dex Server address if it differs from `localhost:8801`.
2. In Dex Web **Connections**, create a `notion-submissions` connection. Follow
   the Notion authorization guide from the connector manifest and share the
   target page and data source with that connection. The sample IDs are inert;
   replace them with IDs from your workspace before making live calls.
3. From `connectors/notion/python`, run
   `uv run python -m examples.submission_intake.worker`.
4. In another terminal, run
   `uv run python -m examples.submission_intake.start <unique-flow-id> examples/submission_intake/sample-input.json`.

For local automated verification, start `dexcli dev -dex-port 22031 -web-port
22032 -open=false` and run
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:22031 uv run pytest -q
examples/submission_intake/integration_test.py
examples/submission_intake/provider_integration_test.py`. The tests use a
real Dex Worker and Client. One uses generated connector mocks; the other
sends all five operations through the production HTTP client to a local
stand-in bound to an OS-selected free port. They cover retries and unwired
optional branch failures, without calling a live Notion workspace.
