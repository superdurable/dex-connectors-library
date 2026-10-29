# ElevenLabs connector

List voices and generate bounded audio through ElevenLabs. The manifest is the source of generated Config, Credentials, Connection, operation factories, and mocks. See the [runnable demo](examples/demo/README.md) for Worker and strict FDG 2.0 generation commands.

## Operations and provider references

| Operation | Dex kind | Official API reference |
| --- | --- | --- |
| `listVoices` | Query | https://elevenlabs.io/docs/api-reference/voices/search |
| `createSpeech` | Query | https://elevenlabs.io/docs/api-reference/text-to-speech/convert |

The demo uses `elevenlabs.NewListVoicesStep` with the static `elevenlabs` connection. Each operation is an operation-specific Query Step. Generation operations use synchronous Execute durability and a three-attempt billed retry budget; read operations use the default one-hour retry policy.

## Authorization and configuration

At [ElevenLabs > Developers > API Keys > Create API Key](https://elevenlabs.io/app/developers/api-keys), create or select an API key for these operations, copy its provider-issued value, and store it in the secret `api_key` field in Dex Web Connections. A blank key is invalid. The manifest supplies the public base URL, HTTP timeout, and response-size limit; leave their fields blank unless a compatible gateway, slow request, or known larger response requires an override.

## Failure behavior

| Condition | Outcome |
| --- | --- |
| 200 with valid bounded output | Happy-path branch |
| 400, 401, 403, or another definite provider rejection | Optional `providerRejected` branch |
| 404 for OpenWeatherMap | Optional `notFound` branch |
| 429, 408, transport error, or 5xx | Retry with the same input until the retry budget ends |
| Malformed or oversized successful response | Optional `invalidResponse` branch |
| Invalid local input or credentials | Optional `defect` branch |

Provider error bodies and secrets are never copied into failures, receipts, or output. Optional branches are unwired in the demo and fail the Flow when selected.

## Mocks

The generated `elevenlabsmock` package uses one consistent default answer per operation:

- `listVoices` default `listedDefault`: `{"voices":[{"voiceId":"voice_meridian","name":"Meridian Narrator"}],"hasMore":false}`
- `createSpeech` default `createdDefault`: `{"audioBase64":"SUQz","contentType":"audio/mpeg"}`

`examples/demo/flow/workflow_mock_integration_test.go` starts a real Dex Flow through the mock connection in CI. Provider unit tests use local HTTP servers for the documented request shape and a rate-limit response.

## Unverified live behavior

The provider voice catalog, plan-specific model access, real audio format, billing, and Dex Web connection form have not been verified with a real key. The audio limit is enforced before base64 encoding.
