# Welcome email example

Configure a Dex Web connection named `resend` with a Resend API key, then run
`npm run example:welcome`. Start `ResendWelcomeEmail` with JSON input containing
`from`, `to`, `subject`, and `html`. The Flow stores only the returned email ID.

For local verification, run `dexcli dev` on a free address and set
`DEX_FLOW_SERVICE_ADDRESS` before `npm run integration:welcome`. This test uses
the generated provider mock through a real Dex Worker and Client.
