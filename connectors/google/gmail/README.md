# Gmail Connector

The Gmail Connector exposes `gmail.NewSendMessageStep`. It sends from the
verified primary address of a dedicated `gmail-send-oauth` Connection.

The OAuth grant requests `openid`, `email`, and `gmail.send`. It does not read
the inbox, sent messages, aliases, or Gmail profile data. The generated
`Credentials` contains a short-lived access token and the verified primary
email; refresh tokens remain in the hosting application's OAuth broker.

The stable Connector idempotency key is written into the RFC `Message-ID` and
safe correlation header. Gmail does not promise server-side deduplication, so
a timeout, connection loss, ambiguous 5xx, or invalid success response returns
the `uncertain` branch and is never automatically resent. Applications must
route that branch to explicit operator recovery.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the Connector release.
