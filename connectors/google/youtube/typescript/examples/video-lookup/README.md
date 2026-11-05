# YouTube video lookup Flow

This Flow reads a video by its 11-character YouTube video ID. It uses the static Dex connection name `youtube-video-reader`. Create that connection in Dex Web with Google OAuth read-only authorization after enabling YouTube Data API v3 for the Google Cloud project. The authorization guide in Dex Web lists the redirect URI, consent, scope, and client setup.

Run `npm run example:video-lookup` to start the Worker. In another terminal, run `npm run example:video-lookup:start -- examples/video-lookup/input.example.json` from the TypeScript package directory. Set `DEX_FLOW_SERVICE_ADDRESS` when the Dex server does not use its default address. The result contains public video details and is persisted as the `youtube-video-result` Attribute.

Run `npm run test:integration` against `dexcli dev` to exercise the same Flow with a local provider stand-in. The test covers a successful read and an unwired `notFound` branch that fails the Flow.
