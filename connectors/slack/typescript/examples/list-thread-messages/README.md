# Read one Slack thread page

Run `npm ci` and `npm run build` in `connectors/slack/typescript`, then configure a Dex project connection named `slack-thread-reader` in Dex Web. The Slack OAuth installation needs the bot and user scopes in the connector README. Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. Run `npx tsx examples/list-thread-messages/main.ts` in one terminal, then `npx tsx examples/list-thread-messages/start.ts '{"channelId":"C123","threadTimestamp":"1710000000.000100","pageSize":15}'` in another. Keep provider tokens in Dex project credentials, never Flow input.

The Flow Definition Graph 2.0 source is [`flow/workflow.ts`](flow/workflow.ts). Its two application Step types each have one `dex:group` and one `dex:explanation` directive. The connector Step uses the static `ConnectionName`; only its `read` branch is wired, so other optional branches fail the Flow. The result Attribute is registered in the persistence schema.
