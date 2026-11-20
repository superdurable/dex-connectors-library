# ZTO waybill tracking example

This Flow reads scans for one authorized waybill and persists the privacy-minimized trace. Its static connection is `zto`.

Start `dexcli dev -open=false`, configure the `zto` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. Replace the sample bill code in `start.ts` with a waybill authorized for the configured application.
