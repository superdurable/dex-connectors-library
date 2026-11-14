# NetEase Yunxin IM user profile

The Go connector performs the current [IM v2 user profile query](https://doc.commsease.com/messaging2/server-apis/TIzNjEzNjc?platform=server). It returns the requested account ID and optional nickname. Email, mobile number, birthday, signature, avatar, gender, and extension are never returned to Dex.

It signs every HTTPS request using the documented [AppKey, Nonce, CurTime, and CheckSum headers](https://doc.commsease.com/messaging2/server-apis/zcwODA3MTU?platform=server). CheckSum is the lowercase hexadecimal SHA-1 digest of `AppSecret + Nonce + CurTime`. The domestic primary host is the default; the backup or overseas host can be configured for an account in that service region.

`getUserProfile` selects `fetched`, `notFound` (provider codes 102404 or 103404), `providerRejected`, `invalidResponse`, or `defect`. Rate limits, transport errors, and temporary provider errors retry. Optional branches fail an example Flow unless wired by an application.

[Run the profile lookup Flow](go/examples/user-profile/README.md). This repository's local mock contains synthetic account data; a live Yunxin account and enabled IM application are required for provider verification.
