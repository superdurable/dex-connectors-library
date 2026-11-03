# Email connector for TypeScript

This package binds the six Email IMAP and SMTP operations to Dex TypeScript Steps. It uses verified TLS for every connection, resolves the app password for every invocation, and keeps connection settings and credentials out of Flow input and state. SMTP sends use a Dex heartbeat marker before submission; a later attempt that sees the marker reports an uncertain outcome rather than sending a duplicate.

Install the package with `npm install @superdurable/connector-superdurable-email @superdurable/dex`. The checked-in [send-message example](examples/send-message/README.md) uses the static `email` connection name and the generated `newSendMessageStep` factory. The package also exports generated Step factories, typed models, branch constants, and scripted mocks for `searchMessages`, `getMessage`, `sendMessage`, `replyToMessage`, `moveMessage`, and `setFlags`.

Configure `imapHost`, `imapPort`, `imapSecurity`, `smtpHost`, `smtpPort`, `smtpSecurity`, `fromAddress`, and `fromName` in Dex Web Connections. Save `username` and `password`, plus `smtp_username` and `smtp_password` only if the SMTP login differs. The [connector setup guide](../README.md#connection-setup) gives provider paths, defaults, limits, and security modes. The connector requires a valid certificate chain; for private test servers, construct `EmailClient` with a PEM `ca` option. This retains host verification.

Run `npm ci && npm run check` from this directory. `npm run test:integration` runs the real SMTPS and IMAPS suite when the CI GreenMail variables are set. The unit suite covers branch decisions, duplicate submission fencing, and protocol classification. The HTTP conformance replay runner exempts this native wire protocol; see `conformance/connectors/README.md`.

No live Fastmail, iCloud, Yahoo, or other provider login or mail delivery has been verified in this port. The GreenMail CI suite covers local SMTPS and IMAPS when available.
