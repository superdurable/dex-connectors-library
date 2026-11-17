# Amazon SES connector

`createEmailIdentity` starts verification of an email address or domain. Supply the exact value under [SES Console > Verified identities](https://console.aws.amazon.com/ses); blank is invalid. Grant `ses:CreateEmailIdentity` for the identity ARN. After an unknown outcome, the connector reads the identity back before resending and caps possible verification requests at three. An address may receive another verification email. Creation does not verify the identity: follow the email link or add the returned public DKIM CNAME records for a domain.

The Go connector signs Amazon SES API v2 requests with AWS Signature Version 4. It provides:

| Operation | Kind | Behavior |
| --- | --- | --- |
| `createEmailIdentity` | Mutation, limited | Start address or domain verification and return public DKIM tokens for a domain. |
| `getEmailIdentity` | Query | Read one identity's verification status. |
| `listEmailIdentities` | Query | List one page of regional identities, with a continuation token. |
| `deleteEmailIdentity` | Mutation, safe | Remove an identity; a missing identity is already deleted. |
| `sendEmail` | Mutation, limited | Send one bounded plain text email from a verified identity. |
| `createEmailTemplate` | Mutation, limited | Create a regional template, reading it back after an unknown outcome. |
| `getEmailTemplate`, `listEmailTemplates` | Query | Read one template or list a page of template names. |
| `updateEmailTemplate`, `deleteEmailTemplate` | Mutation, safe | Replace all template content or delete a template. |
| `sendTemplatedEmail` | Mutation, limited | Send a stored template with JSON variables. |

Queries and deletion retry transport failures, throttling, and server failures. `sendEmail` uses synchronous Execute and a Dex heartbeat fence. SES offers no idempotency key or message read-back for this call, so an unknown result can cause a duplicate email. The connector resends at most three times and selects `uncertain` when the cap is reached without an accepted message ID. A definite rejection selects `providerRejected`.

In [Amazon SES Console](https://console.aws.amazon.com/ses), open **Verified identities**, select the email address or domain, and use the console Region selector for `region`. Enter the exact identity value as `emailIdentity` in the lookup Flow input. In [IAM Console](https://console.aws.amazon.com/iam), open **Users > selected user > Security credentials > Access keys > Create access key**. Grant `ses:GetEmailIdentity`, `ses:CreateEmailIdentity`, and `ses:DeleteEmailIdentity` for selected identity ARNs; `ses:ListEmailIdentities` for listing; `ses:CreateEmailTemplate`, `ses:GetEmailTemplate`, `ses:ListEmailTemplates`, `ses:UpdateEmailTemplate`, and `ses:DeleteEmailTemplate` for stored templates; and `ses:SendEmail` for a verified sender identity when sending. Copy the access key ID and one-time secret access key into their respective secret fields; set `session_token` for temporary credentials. Leave `endpoint` blank for the regional AWS API. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. Rotate credentials through IAM when needed.

For `sendEmail`, supply a verified `fromEmailAddress`, one `toEmailAddress`, a one-line UTF-8 `subject` of at most 998 bytes, and a UTF-8 `textBody` of at most 100 KiB. SES sandbox accounts can send only to verified recipients or the mailbox simulator. The returned message ID confirms acceptance, not delivery.

For templates, open [SES Console > Email templates](https://console.aws.amazon.com/ses) in the same Region as the connection. Use the template name exactly as shown there. A template needs a subject and at least one text or HTML body; `updateEmailTemplate` replaces the complete content. `sendTemplatedEmail` takes one verified sender, one recipient, the stored template name, and a JSON object of template variables. SES cannot deduplicate a send, so the connector caps possible sends at three and may return `uncertain`.

The [Go example](go/examples/identity-lookup/README.md) lists, looks up, verifies, and sends through four runnable Flows. The independently versioned [Python](python/examples/identity_lookup/README.md) and [TypeScript](typescript/examples/identity-lookup/README.md) packages still implement only `getEmailIdentity`. Tests use local HTTP fixtures; live AWS authorization and calls have not been verified.
