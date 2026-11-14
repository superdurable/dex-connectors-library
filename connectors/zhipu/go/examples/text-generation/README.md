# Zhipu text generation Flow

Run the Worker from this module after starting `dexcli dev`:

```sh
GOWORK=off go run ./examples/text-generation
```

Create a Dex Web connection named `zhipu` using the [connection guide](../../../README.md#connection). Start the `ZhipuTextGeneration` Flow with input such as `{"prompt":"Summarize this release in one sentence.","maxOutputTokens":256}`. The `generated` branch completes with text, model, response ID, finish reason, and token usage. A bounded local mock can be verified with `GOWORK=off go test -tags integration ./examples/text-generation/flow -run TestTextGenerationUsesDefaultMockWithRealDex` while `dexcli dev -dex-port 8971 -web-port 8972 -open=false` is running.

Validate its Flow Definition Graph 2.0 with:

```sh
dexcli visualize ./examples/text-generation/flow/workflow.go --schema-version 2.0 --json --out /tmp/zhipu-text-generation
```
