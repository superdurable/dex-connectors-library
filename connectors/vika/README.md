# Vika

The Go connector lists workspaces available to the configured Vika account. `ListSpaces` returns each workspace's ID, name, and administrator flag. It does not read datasheet records. The operation has no input fields.

Create an API Token in [Vika](https://vika.cn) under the lower-left profile menu → User Center → Developer Configuration → `+`. Vika may first require an email address. Enter the token in the secret Dex Web connection field without `Bearer`. It inherits the account's workspace permissions. See the [official quick start](https://developers.vika.cn/api/quick-start/) and [API reference](https://developers.vika.cn/api/reference/).

The connector calls `GET https://vika.cn/fusion/v1/spaces` over HTTPS. The free plan documents two requests per second per user and datasheet; higher tiers permit more. Rate limits and transient server failures are retried by Dex. No live account authorization has been verified.

The [runnable Dex Flow](go/examples/space-list/README.md) shows connection setup, local mock verification, and Flow output. The logo is the official Vika SVG from the [APITable source repository](https://github.com/apitable/apitable/blob/develop/packages/datasheet/public/logo_text_dark.svg).
