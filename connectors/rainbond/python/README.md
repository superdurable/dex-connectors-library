# Rainbond Python connector

`dex-connector-rainbond` implements the `listTeams` Query with the same provider decisions as Go. See [the connector guide](../README.md) for connection setup. Seven Go-recorded scenarios verify success, terminal branches, 429, 503, and transport failure. The runnable [Flow](examples/current_query/) verifies retry and persistence through a real local Dex Worker and Client. Live provider authorization is unverified without an approved account.
