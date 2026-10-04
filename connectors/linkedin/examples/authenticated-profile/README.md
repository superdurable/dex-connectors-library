# LinkedIn authenticated-profile example

This example loads the authenticated LinkedIn member's bounded OpenID Connect
UserInfo profile in a Flow started from Dex Web **Start Flow**.

Generate strict FDG 2.0 from `connectors/linkedin`:

```bash
mkdir -p build
dexcli visualize ./examples/authenticated-profile/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/authenticated-profile
```

Run `dexcli dev` with that build directory, configure
`linkedin / linkedin-profile` under **Connections**, then start the Worker. It
reads the `DEX_PROJECT_*` project configuration environment described in
[project configuration](../../../../sdkgo/projectconfig/README.md); Dex Web or
Superverse Studio writes that configuration when you save the connection.

```bash
go run ./examples/authenticated-profile
```

Start `LinkedInAuthenticatedProfile` with a unique Flow ID:

```json
{"requestLabel":"signup-profile"}
```

Only `profileLoaded` is wired. Missing verified email, insufficient scope,
revoked authorization, not-found, provider rejection, invalid responses, and
local defects fail the Flow.

```bash
GOWORK=off go test -race ./examples/authenticated-profile/...
```
