# Unisound connector

This connector lists predefined text-to-speech voices from [Unisound MaaS](https://maas.unisound.com/docs/api/voice/get-voice). `ListSystemVoices` sends `POST https://maas-api.unisound.com/v1/audio/voices/query` with `voice_type=system` and returns up to 25 voice IDs, names, and language codes. It excludes cloned and designed voices and does not synthesize audio. Unisound's [platform](https://maas.unisound.com/) is operated by the Chinese company 云知声.

## Connection

Sign in at [Unisound MaaS](https://maas.unisound.com/docs/guide/quickstart), complete developer verification, then open 控制台 > 按量计费 > API Key 管理 > 创建 API Key. Copy the secret API Key into Dex Web Connections. The U Plan Key is distinct from the on-demand Key; use one authorized for voice catalog access. Leave timeout and maximum response size blank to use the displayed defaults.

Run the [example Flow](go/examples/list-system-voices/) with `{}` input. The checked-in mock returns one system voice. Live Unisound authorization and catalog behavior require a user-provided Key and have not been verified here.
