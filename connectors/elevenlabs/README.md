# ElevenLabs connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/elevenlabs/go@v0.3.0` after release.

Moved from `github.com/superdurable/dex-connectors-library/connectors/elevenlabs`.

List and inspect voices, replace voice settings, and generate bounded audio through ElevenLabs. The manifest is the source of generated Config, Credentials, Connection, operation factories, and mocks. See the [runnable demo](go/examples/demo/README.md) for Worker and strict FDG 2.0 generation commands.

## Operations and provider references

| Operation | Dex kind | Official API reference |
| --- | --- | --- |
| `listVoices` | Query | https://elevenlabs.io/docs/api-reference/voices/search |
| `getVoice` | Query | https://elevenlabs.io/docs/api-reference/voices/get |
| `updateVoiceSettings` | Mutation, safe repeat | https://elevenlabs.io/docs/api-reference/voices/settings/update |
| `createSpeech` | Query | https://elevenlabs.io/docs/api-reference/text-to-speech/convert |

The demo uses operation-specific `elevenlabs.NewListVoicesStep` and `elevenlabs.NewUpdateVoiceSettingsStep` factories with the static `elevenlabs` connection. `updateVoiceSettings` sends five absolute values, so retrying the same input preserves the chosen settings. Generation uses synchronous Execute durability and a three-attempt billed retry budget; reads and settings replacement use the default one-hour retry policy.

The Python package is [`dex-connector-elevenlabs`](python/README.md), imported as
`dex_connectors.elevenlabs`.

The TypeScript package is [`@superdurable/connector-elevenlabs`](typescript/README.md).
Its [runnable Flow](typescript/examples/demo/README.md) and conformance suite cover
the generated TypeScript Step factories and both provider operations.

## Authorization and configuration

At [ElevenLabs > Developers > API Keys > Create API Key](https://elevenlabs.io/app/developers/api-keys), create or select an API key with voice read, voice write, and speech generation permissions for the operations used, copy its provider-issued value, and store it in the secret `api_key` field in Dex Web Connections. A blank key is invalid. The manifest supplies the public base URL, HTTP timeout, and response-size limit; leave their fields blank unless a compatible gateway, slow request, or known larger response requires an override.

## Failure behavior

| Condition | Outcome |
| --- | --- |
| 200 with valid bounded output | Happy-path branch |
| 400, 401, 403, or another definite provider rejection | Optional `providerRejected` branch |
| 429, 408, transport error, or 5xx | Retry with the same input until the retry budget ends |
| Malformed or oversized successful response | Optional `invalidResponse` branch |
| Invalid local input or credentials | Optional `defect` branch |

Provider error bodies and secrets are never copied into failures, receipts, or output. Optional branches are unwired in the demo and fail the Flow when selected.

`getVoice` additionally selects `notFound` for a missing voice. `updateVoiceSettings` selects `updated`, `notFound`, `providerRejected`, or `defect`; an interrupted or unusable success confirmation retries the same absolute settings.

## Mocks

The generated `elevenlabsmock` package uses one consistent default answer per operation:

- `listVoices` default `listedDefault`: `{"voices":[{"voiceId":"voice_meridian","name":"Meridian Narrator"}],"hasMore":false}`
- `getVoice` default `foundDefault`: `{"voiceId":"voice_meridian","name":"Meridian Narrator"}`
- `updateVoiceSettings` default `updatedDefault`: `{"voiceId":"voice_meridian"}`
- `createSpeech` default `createdDefault`: `{"audioBase64":"SUQz","contentType":"audio/mpeg"}`

`examples/demo/flow/workflow_mock_integration_test.go` starts a real Dex Flow through the mock connection in CI. Provider unit tests use local HTTP servers for the documented request shape and a rate-limit response.

## Unverified live behavior

The provider voice catalog, plan-specific model access, real audio format, billing, and Dex Web connection form have not been verified with a real key. The audio limit is enforced before base64 encoding.
