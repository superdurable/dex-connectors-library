# Microsoft Teams Python connector

`dex-connector-microsoft-teams==0.1.1` provides the four Microsoft Teams operations for Dex Flows. Configure a named Microsoft Teams connection in Dex Web before running a Flow. Live Microsoft authorization and provider calls have not been verified.

Posts follow the Go connector's duplicate-safe posting rules: an unknown outcome is read back when possible and resent only while possible sends stay below `max_sends` (default 3), and `uncertain` is selected only at that cap or when several matching messages are found.

Decoded sender IDs and Graph request IDs retain printable values up to 128 bytes; message type, importance, and content type are limited to 64 bytes.
