# TDengine Cloud connector

This connector lists accessible database metadata through TDengine Cloud's documented [REST API](https://docs.tdengine.com/cloud/programming/client-libraries/rest-api/). It posts only the fixed statement `select name, ntables, status from information_schema.ins_databases;` to `/rest/sql`; Flow callers cannot supply SQL. Results contain the database name, table count, and status. Provider descriptions, tokens, and URLs are excluded from outputs, receipts, and failures.

In [TDengine Cloud](https://cloud.tdengine.com/), select an instance and open **Programming → REST API** to obtain its HTTPS Cloud URL and per-user, per-instance Cloud Token. Enter the URL's origin and the token in the Dex Web connection. The token travels in the provider-documented `token` query parameter over HTTPS. Redirects are disabled so it cannot be forwarded to another host. Use a token with only the database privileges needed for this metadata query.

The runnable [list-databases Flow](go/examples/list-databases/README.md) uses the generated operation-specific Step and static `taosdata` connection. Transient HTTP/transport failures retry under Dex's default one-hour retry budget; definite provider rejections select the optional `providerRejected` branch. Real TDengine Cloud authorization and result shapes remain to be verified with a user-controlled connection.
