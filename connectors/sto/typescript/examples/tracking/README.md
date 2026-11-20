# STO waybill tracking example

This Flow reads scans for one authorized STO waybill and persists the privacy-minimized trace. Its static connection is `sto`.

Start `dexcli dev -open=false`, configure the `sto` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. Replace the sample waybill number in `start.ts` with one authorized for the application.
