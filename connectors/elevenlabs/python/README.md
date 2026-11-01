# ElevenLabs connector for Python

Install with `pip install dex-connector-elevenlabs==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listVoices` and `createSpeech` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listVoices` | `new_list_voices_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `createSpeech` | `new_create_speech_step` | `created` | `providerRejected`, `invalidResponse`, `defect` |

`listVoices` reads one page of `/v2/voices` with an optional page size from 1
through 100 and the preceding page's token. `createSpeech` posts text to
`/v1/text-to-speech/<voiceId>` with `output_format=mp3_44100_128`, defaults the
model to `eleven_multilingual_v2`, and returns the bounded MP3 as base64.

Generation is billed, so `createSpeech` uses sync durability, a 150-second
Execute timeout, and at most three attempts within five minutes. Both
operations retry on 408, 429, 5xx, and transport failures; any other non-200
status selects `providerRejected`.

## Authorization

Create a Dex Web connection named `elevenlabs` as described in the
[connector README](../README.md#authorization-and-configuration). The API key
travels only in the `xi-api-key` header and is resolved for every attempt.

## Example

[`examples/demo`](examples/demo/flow.py) lists one page of voices. Run the
Worker with `uv run python -m examples.demo.worker` after `dexcli dev` has
loaded a project containing the `elevenlabs` connection.

## Tests

`elevenlabsmock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
