# Youdao text translation Flow

This runnable Flow calls `translateText` through the static `youdao` connection, stores the returned translation, and completes with a typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/youdao/go`, run:

```bash
dexcli visualize ./examples/text-translation/flow/workflow.go --schema-version 2.0 --json --out /tmp/youdao-text-translation-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override youdao=<artifact-directory>`. In Dex Web Connections select `youdao` and follow the [connection guide](../../../README.md) to enter the App Key and secret App Secret. Leave configuration fields blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/youdao/go`:

```bash
GOWORK=off go run ./examples/text-translation
```

Start `YoudaoTextTranslation` with a unique Flow ID and input `{ "text": "Hello", "sourceLanguage": "en", "targetLanguage": "zh-CHS" }`. The Worker connects to Dex Server without an inbound listener. The real-Dex integration test runs with a mock connection. Live provider translation remains unverified.
