# ElevenLabs demo Flow

This runnable Flow calls `listVoices` with the static `elevenlabs` connection. The start input passes through to the generated operation Step; its happy-path result completes the Flow. Dex Web summary and display RPCs show the persisted provider result. Optional provider branches are unwired and fail the Flow when selected.

The Worker also registers `ElevenLabsVoiceSettings`. Start it with `voiceId`, `stability`, `similarityBoost`, `style`, `speed`, and `useSpeakerBoost`; the five values replace the target voice's generation settings. `TestVoiceSettingsUsesDefaultMockWithRealDex` exercises this Flow against a real local Dex with the connector mock.

The Worker registers `ElevenLabsModels`. Start it with `{}` to list available model IDs, names, speech support, and input limits. `TestModelsUsesDefaultMockWithRealDex` exercises it against real local Dex with the connector mock. Optional provider branches are unwired.

## Strict FDG 2.0 generation

From `connectors/elevenlabs/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/demo/flow/workflow.go --schema-version 2.0 --json --out ./build/demo
dexcli visualize ./examples/demo/flow/voice_settings_flow.go --schema-version 2.0 --lint app --json --out ./build/voice-settings
dexcli visualize ./examples/demo/flow/models_flow.go --schema-version 2.0 --lint app --json --out ./build/models
```

The command must report `valid: true`. A local unpublished module may report `connector_release_required` until its release metadata is supplied.

## Run with Dex Web

Create the connector release artifact from this source, pass it to `dexcli dev` with `--connector-release-override elevenlabs=<artifact-directory>`, and use the generated `build` Flow rendering. Open Dex Web Connections, select `elevenlabs / elevenlabs`, and follow the key guide at https://elevenlabs.io/app/settings/api-keys. Leave the base URL and safety limits blank for defaults.

From `connectors/elevenlabs/go`, with the project's `DEX_PROJECT_*` environment set, run:

```bash
GOWORK=off go run ./examples/demo
```

In Dex Web Start Flow, choose `ElevenLabsDemo` and enter a unique Flow ID with this input:

```json
{"pageSize":10}
```

The pull Worker connects to the Dex Server without an inbound listener. The Flow returns `ListVoicesOutput`. The provider connection name is fixed in `flow/workflow.go`.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test -race ./...` from the connector directory.
