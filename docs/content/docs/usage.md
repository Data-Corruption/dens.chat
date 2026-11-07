---
title: Usage
weight: 2
aliases:
  - /docs/join/
  - /docs/logs/
---

Dens is built for independence: there are no central servers, and nothing
you need from its developer once it's installed. That's great for many
reasons, but it has limits. Each den is fully isolated: there's no
federation. If you have the same friend in two dens, you have two sets of DMs
with them, and if a den shuts down, you lose touch with everyone who was only
there. For friends you'd like to stay in touch with, share some other way to
reach each other, or host your own den.

## Joining

A **den** is one community: its channels, its members and their DMs, on a
computer run by one of them. You need an **invite** from someone there, a long
code starting with `dens1:`.

1. Open Dens and choose **Add a den**.
2. Paste the invite, and choose **Join as someone new**.
3. Pick a username, a display name and a password for this den. The username
   can't change.
4. Save your recovery codes and your DM recovery key somewhere safe, such as a
   password manager.

You have a separate profile in each den, and no den can link it to your
others. Each den has a **Den ID**, which everyone in it sees the same:
compare it with someone already there to be sure you joined the den you
meant.

## Your passwords and keys

There's no company keeping your account for you, so Dens asks you to keep a few
things most apps don't.

| | What it's for | Good to know |
| --- | --- | --- |
| **Local password** | Protects Dens on this computer: backups, and seeing your keys | It never leaves this computer |
| **Den password** | Signing in to a den from another device | One per den. The den never sees it, so reusing one across dens is safe |
| **Recovery codes** | Signing in when you've forgotten your den password, or have no other device | Ten per den, and each works once. You can make new ones in **Manage** |
| **DM recovery key** | Reading your DMs and notes if you lose every device | The same for every den on this computer. Keep it secret: it opens your DMs |

You'll find the last three again under the den's **Manage**, in **You in this
den**, and the local password in **Settings**, under **System**.

## Signing in from another computer

Every computer runs its own Dens. On the second one, choose **Add a den**,
paste an invite to the den, even a used one, or the den's address, and choose
**Sign in to my account there**. Enter your username and den password, then
approve the sign-in from a device that's already in: type the digits each
screen shows into the other. That proves the new device is the one in front
of you, and hands it what it needs to read your DMs.

No other device at hand? Use a recovery code instead, then enter your DM
recovery key to read your DMs. Forgot your den password? Choose **Forgot your
password? Use a recovery code** when signing in: you'll set a new one, and
your other devices are signed out.

If a sign-in you didn't start asks for approval, refuse it, and change your den
password: whoever asked has it.

## Direct messages

DMs are end-to-end encrypted: only you and the other person can read them, not
the den. Open someone's profile and choose **Message**; it opens at once.

The den passes your keys between you, so if it were tampered with, it could
slip in keys of its own. To be sure it didn't, compare codes.

### Comparing codes

Open the lock beside the person's name in the DM, or **Compare codes** on their
profile. Read your code out to them, and type theirs as they read it out. If
both match, nobody sits between you, and a check shows beside their name.

Compare somewhere the den can't reach: in person, on a call where you know
it's really them, or in a den one of you owns. Not this one, even if it's
yours: whatever tampered with it could change the digits there too. Be wary
of "I'm @them over there, let's compare there" for the same reason. If the
digits don't match, don't trust the DM until you've compared somewhere else.

{{% details title="Your code, calls and the check" closed="true" %}}

Each of you has a code in each den, 20 digits, the same for everyone who
compares with you. Your own code is in the den's **Manage**, under **Your
code**. Comparing is up to you, any time, and the den never learns whether you
did. The check shows on your other devices too, within the hour.

Calls are end-to-end encrypted too, with the same codes: the lock beside a call
shows yours, and everyone in the call, to compare with right there. A
comparison holds wherever you meet them after, in DMs, groups and calls.
Whether the check shows beside people in lists is yours to choose, under
**Settings** → **Appearance** → **Verified status**.

If someone's keys change, as when they reset their DM recovery key, the DM says
so where it happened, and their check goes. If you'd compared codes with
them, the DM waits until you compare again, or choose **Send anyway**.

{{% /details %}}

### Group DMs

A group DM is a DM of three to ten people, end-to-end encrypted as a DM is.
Start one from **+ New group** above your DMs, or from **Add people** on the
profile beside a DM. Someone added reads it from when they joined, not before,
and someone removed, or who left, reads nothing written after. Anyone in it can
add people, rename it or leave; whoever made it can remove people. Each change
is signed by the member who made it, so the den can't add anyone on its own.
Groups have no calls.

### Disappearing messages

Anyone in a DM or a group DM can have its messages disappear 1 hour, 1 day, 1
week or 4 weeks after they're sent: pick a time under **Disappearing
messages**, on the profile beside a DM or below a group's members. When its
time comes, a message is gone from the den and from everyone's devices, even
ones that were offline, with its photos and files. Anyone in the conversation
can still copy or screenshot it before then.

### Notes to self

Your notes are a conversation with yourself, at the top of your DMs: links,
lists, drafts and files you want on each of your devices. They're end-to-end
encrypted with a key made from your DM recovery key, so only your devices can
read them. Resetting that key, or leaving the den, loses them.

## Around the den

Channels, calls, screen shares, reactions, polls, emoji, events and the
soundboard work as you'd expect. What's different:

- **Calls** are end-to-end encrypted, and the lock beside one lists who's in
  it, to compare codes with. Everyone in a call hears the soundboard, and
  one sound plays at a time.
- **Polls** can be anonymous to everyone in the den, its owner included; the
  den itself still knows, since it counts each vote once. With **After
  voting**, each person sees the totals once they vote, and their vote is
  then final.
- **Reactions** in a DM or a group are sealed like its messages: the den
  sees that yours changed, not what they are.
- **Spoilers**: choose **Spoiler** on a photo or video before you send it, or
  write `||text||`.
- **Events**, at the top of a den's channel list: choose **☆ Interested** and
  Dens tells you as one starts, on your phone too. Be in a voice-channel
  event's call for a minute and you earn its badge, which shows on your
  profile; hide any badge with its eye on your profile. Nobody sees who's
  interested, only how many.
- **Your profile** is per den. A crown marks the den's owner, a shield a
  moderator. Photos you send lose their location and camera details before
  they leave your computer.
- **Compact messages**, a line each, is under **Settings** → **Appearance**
  → **Message density**.

## Notifications

Nothing notifies you until you turn it on: in **Settings**, under
**Notifications & sounds**, turn on **Direct messages**, **Mentions**, or
both. The bell beside a den's name mutes the den, and the bell at the top of a
channel or DM mutes just that one. A notification shows the start of the
message to anyone who can see your screen; turn off **Show the message** and
it says only who sent one.

{{% details title="Where they show" closed="true" %}}

- **A Dens tab, anywhere in your browser.** Turning one on asks the browser to
  let Dens show notifications; say yes. If you said no, allow notifications
  for `localhost` in the browser's site settings.
- **No Dens tab open.** Dens's helper, which starts when you sign in, shows
  them as your desktop's own, and clicking one opens Dens.
- **Android.** The app shows them while it isn't open on that channel. If you
  didn't allow notifications when Android asked, allow them for Dens in
  Android's settings, under Apps.

Reading a channel on any of your devices clears its notification everywhere.
The settings live in Dens on your computer, so every browser you use shares
them. Moderators and the owner can write `@everyone`; turn off **@everyone**
under **Mentions** if that's too much. Each sound is under **Sounds** there,
to turn off, turn down, or replace with a file of your own.

{{% /details %}}

## Leaving

Open the den's **Manage**, from the den's name at the top of its channel list,
and choose **Leave den** at the bottom. You can come back with a new invite, as
yourself: join with your old username and den password, and your history comes
back with you. Your notes don't.

If Dens can't reach the den, because its server is gone for good, say,
**Leave den** offers **Remove from this computer** instead: it deletes your
keys for the den on this computer, and the messages and files it kept. If the
den comes back, sign in again from **Add a den**.

## Sending logs

When something goes wrong, such as a call that keeps dropping, Dens's logs say
what it did and when. In **Settings**, under **System**, set **Log level** to
**debug**, let it happen again, then **Download the newest logs** there and
send the file. Set the level back to **warn** after: at debug the logs grow
faster.

The logs hold no messages, keys, passwords or network addresses. They name
your dens, and channels and members by number, and they're plain text, so you
can read them before you send them. For a call that drops, the den's logs
help too: its owner gets them the same way, or on a server with
[`dens logs`](../hosting/#from-the-command-line).
