# Mureka task-status example

This Flow reads an existing Mureka song task and persists only its public status fields. Its static connection is `kunlun`.

Start `dexcli dev -open=false`, configure the `kunlun` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. The start script uses a sample task ID; set it to an ID from your own song-generation response before using the real provider.
