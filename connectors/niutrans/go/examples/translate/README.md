# NiuTrans translation Flow

This runnable Flow uses the static `niutrans` connection to translate one bounded text string. The `translated` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Run from this Go module:

```sh
go run ./examples/translate
```

In Dex Web **Connections**, create the `niutrans` connection. Sign in at [NiuTrans](https://niutrans.com), open **控制台 > 个人中心 > APIKEY**, complete any required account binding, then reveal and copy the key into the secret field. Leave the API base URL, timeout, and response size blank for their displayed defaults. Translation consumes points based on source characters.

For local testing, build the connector release artifact and pass it to `dexcli dev --connector-release-override niutrans=<artifact-directory>`. The `sampleTranslation` mock returns synthetic text without calling the provider.

In Dex Web **Start Flow**, choose `NiuTransTranslation`, supply a unique Flow ID, and enter:

```json
{"text":"你好。","from":"zh","to":"en"}
```

Generate the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/translate/flow/workflow.go --schema-version 2.0 --json --out /tmp/niutrans-translation-fdg
```
