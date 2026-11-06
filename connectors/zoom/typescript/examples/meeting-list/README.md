# Zoom meeting list example

This Flow lists one page of meetings for the authorized Zoom user through the static `zoom-scheduler` connection. It persists the request and the safe meeting page as Attributes. The Worker entrypoint is `main.ts`; the real Dex Worker and Client check is `integration.ts`.

Configure the OAuth connection in Dex Web using the guide in `connector.yaml`, then run `npm run example:meeting-list` with `DEX_FLOW_SERVICE_ADDRESS` set to your Dex endpoint. Start a `ZoomMeetingListFlow` with `{ "type": "upcoming", "pageSize": 30 }`.

For a local stand-in, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=<address> npm run example:meeting-list:integration`. This uses the generated mock and tests retry without Zoom credentials.
