# Baichuan AI knowledge-base list Flow

This runnable Flow uses the static `baichuan` connection to list knowledge
bases. The `listed` branch stores the provider result in a Dex Attribute and
completes with the knowledge bases. Optional branches are unwired and fail the
Flow when selected.

## Strict FDG 2.0 generation

From `connectors/baichuan/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/knowledge-base-list/flow/workflow.go --schema-version 2.0 --json --out ./build/knowledge-base-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to
`dexcli dev --connector-release-override baichuan=<artifact-directory>`.
In Dex Web **Connections**, create the `baichuan` connection with a secret
API Key from an approved [Baichuan API account](https://platform.baichuan-ai.com/).
Leave base URL, timeout, and response size blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/knowledge-base-list
```

In Dex Web **Start Flow**, choose `BaichuanKnowledgeBaseList`, provide a
unique Flow ID, and use empty JSON input:

```json
{}
```

The output contains `knowledgeBases`. A successful empty list means the
provider returned none. Mock data is a sample, not a claim about an account.
The mock-backed integration test uses a real Dex Worker and Client without
a Baichuan credential.
