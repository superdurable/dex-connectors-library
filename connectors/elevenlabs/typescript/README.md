# ElevenLabs TypeScript connector

`@superdurable/connector-elevenlabs` provides two Dex Query operations:
`listVoices` and `createSpeech`. The manifest generates the public models,
operation definitions, Step factories, connection bindings, and scripted mocks.
The provider client resolves the API key for each attempt and never puts it in
Flow input or results.

The [runnable demo](examples/demo/README.md) shows the complete Step and Flow
wiring. `listVoices` returns a bounded voice page. `createSpeech` returns
base64 MP3 data; it uses a three-attempt retry budget because generation may
be billed again. Both operations retry transport failures, 408, 429, and 5xx.
Other HTTP rejections select `providerRejected`, malformed or oversized 200
responses select `invalidResponse`, and invalid local input or credentials
select `defect`. Unwired optional branches fail the Flow.

Run `npm ci && npm run check` to verify types, lint, conformance replay, unit
tests, and the package build. With `dexcli dev` running, set
`DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration` for a real Worker
and Client test against a local provider fixture. The checked-in Go conformance
recording supplies exact request and output parity for both operations.

The real ElevenLabs voice catalog, model access, audio format, billing, and
Dex Web Connections form have not been verified with a live provider key.
