# WeCom TypeScript examples

These runnable Dex Flows use static connections `wecom-robot` and `wecom-app`. Configure them in Dex Web with the corresponding group robot and self-built application methods. Start `dexcli dev -open=false`, run `npm run example:worker`, then run either:

```bash
npm run example:start -- robot '{"messageType":"text","content":"Deployment complete"}'
npm run example:start -- application '{"toUserIds":["zhangsan"],"messageType":"text","content":"Incident INC-7 opened"}'
```

Credentials stay in Dex Connections. The Flow persists only provider acceptance, never the message content or credentials. Optional refusal and uncertain branches are unwired and fail the Flow if selected.
