# YouTube video search Python Flow

Create a Dex Web connection named `youtube-playlist-curator`. Follow the Google OAuth setup guide shown by Dex Web, enable YouTube Data API v3, and authorize the `youtube.readonly` scope. Set `DEX_FLOW_SERVICE_ADDRESS` to your `dexcli dev` gRPC address, then run `uv run python -m examples.video_search.main` from this package. Start `YouTubeVideoSearchPython` with input such as `{"query":"compiler","channelId":"UC_x5XG1OV2P6uZZ5FSM9Ttw"}`. The Flow stores one public search page and completes; optional branches are unwired and fail the Flow when selected.

Run `uv run pytest examples/video_search/integration_test.py` while `dexcli dev` is running. The test uses an inert provider stand-in on an OS-assigned port and a real Dex Worker and Client. Live Google OAuth and YouTube requests remain unverified.
