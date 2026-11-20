# Youdao TypeScript example

This runnable Flow calls `translateText` with static connection name `youdao` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '{"text":"你好","sourceLanguage":"zh-CHS","targetLanguage":"en"}'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.
