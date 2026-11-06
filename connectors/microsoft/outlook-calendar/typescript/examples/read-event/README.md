# Read an Outlook Calendar event

This checked-in Dex Flow reads one Microsoft Graph event with the static connection name `outlook-calendar-scheduler`. Configure that name in Dex Web Connections, then run `npm run example:worker` and start `OutlookCalendarReadEventExample` with `eventId` and an optional IANA `timeZone`.

For local verification, start `dexcli dev -dex-port 40722 -web-port 40723 -open=false` and run `npm run test:integration`. The integration test uses a fake Graph transport through a real Dex Worker and Client. Live Microsoft authorization has not been verified.

The Flow source declares one `dex:group` and one `dex:explanation` for its completion Step. Generate and lint its strict FDG 2.0 source after building the package:

```sh
cd connectors/microsoft/outlook-calendar/typescript
npm run build
mkdir -p examples/read-event/node_modules/@superdurable
ln -s ../../../.. examples/read-event/node_modules/@superdurable/connector-microsoft-outlook-calendar
dexcli visualize examples/read-event/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/outlook-calendar-ts-fdg
```

The `node_modules` link is ignored by Git and lets Dex CLI read the generated connector manifest before the package is published.
