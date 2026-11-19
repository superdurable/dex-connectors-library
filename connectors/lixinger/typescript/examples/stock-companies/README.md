# Lixinger stock-company example

This Flow reads basic listing metadata for selected A-share stock codes. Its static connection is `lixinger`.

Start `dexcli dev -open=false`, configure the `lixinger` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. Replace the sample stock codes in `start.ts` with your own six-digit codes for a real provider query.
