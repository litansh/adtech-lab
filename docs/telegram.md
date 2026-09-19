# The Telegram channel

Where the agents report and where you decide.

---

## What it is

**Outbound: the agents post.** When an agent opens a pull request or an issue,
a message arrives on your phone with the finding and a button to the GitHub
page. A nightly report committed to the repository is a log nobody opens; a
message on a phone gets read.

**Authority: GitHub.** The message links to the pull request. You approve there
— two taps in the GitHub mobile app.

## Why orders do not go through the bot

You asked for somewhere to discuss and give orders. Discussion works here.
**Orders deliberately do not**, and it is worth being explicit about why rather
than quietly not building it.

A bot that accepts commands is a webhook on the public internet that triggers
production changes. To make it safe you would need, at minimum:

- a secret token on the webhook path, checked on every request
- the sender's Telegram user id checked against an allow-list — a channel can
  be joined, and a forwarded message can be crafted
- replay protection, since Telegram retries deliveries
- a bounded command set, because "run this" is a remote shell with extra steps
- an audit trail separate from Telegram, which anyone with the token can post to

That is a real amount of security work for a convenience GitHub's mobile app
already provides. The messages carry a button that opens the pull request; the
approval happens where the audit trail already lives and where the permission
model is already correct.

**If we ever build the inbound side**, the design is: Telegram → API Gateway →
Lambda, verifying the secret token and the sender id, translating a small fixed
vocabulary into `workflow_dispatch` calls, and refusing anything not in that
vocabulary. Never a shell, never a free-text instruction, and never an approval
— approving your own agent's change from a chat app removes the one place a
second pair of eyes was supposed to happen.

## Setting it up

Two values, both of which only you can create.

**1. Make the bot.** In Telegram, message `@BotFather`, send `/newbot`, follow
the prompts. It replies with a token like `123456:ABC-DEF...`.

**2. Make the channel and get its id.** Create a channel, add the bot as an
administrator, post any message in it, then open:

```
https://api.telegram.org/bot<TOKEN>/getUpdates
```

The chat id is in the response and starts with `-100`.

**3. Give them to CI:**

```
gh secret set TELEGRAM_BOT_TOKEN --body "<token>"
gh secret set TELEGRAM_CHAT_ID   --body "-100..."
```

Nothing else changes. `tools/notify/telegram.py` prints *"not configured"* and
exits cleanly when the secrets are absent, so the agents run identically
whether or not anyone is listening — a notifier that can fail a job is worse
than no notifier.

## A second use worth considering

The same channel could be **public**, and the bot could post the Daily Puzzle
every morning with a link. That turns a notification channel into a
distribution channel, and a Telegram channel is one of the few places where a
daily habit and a share button already live together.

It is a different product decision from this one, and it should wait until the
daily has any audience at all — see [audience.md](audience.md).
