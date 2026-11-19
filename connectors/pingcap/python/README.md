# PingCAP Python connector

`dex-connector-pingcap` implements `listProjects` with HTTP Digest authentication. Configure the public and private API keys from the [TiDB Cloud console](https://tidbcloud.com/console) in Dex Web Connections. The Query returns a bounded project page without exposing keys.

See the [runnable project-list Flow](examples/project_list/). Go-recorded conformance scenarios cover both Digest requests and provider decisions. The checked-in Dex integration test uses a fake provider; live TiDB Cloud authorization and retrieval remain unverified.
