---
title: Join a den
weight: 2
---

A **den** is one community on Dens: its channels, its members and their DMs,
all on a computer run by one of them. To join one you need Dens
[installed](../install/), and an **invite** from someone there: a long code
starting with `dens1:`.

## Joining

1. Open Dens (`dens open`) and choose **Add a den**.
2. Paste the invite, and choose **Join as someone new**.
3. Pick a username, a display name and a password for this den.
4. Save your recovery codes and your DM recovery key somewhere safe, such as a
   password manager. The next section says what they're for.

Your **username** is how people mention you, and it can't change, so pick one
you'll keep: 2 to 32 characters, from lowercase letters, numbers and `_`. Your
**display name** is what people see, and you can change it any time. You have a
separate profile in each den, and no den can link it to your others.

Each den has a **Den ID**, which everyone in it sees the same. Comparing it with
someone already in the den tells you that you both joined the same one.

## Your passwords and keys

There's no company keeping your account for you, so Dens asks you to keep a few
things most apps don't.

| | What it's for | Good to know |
| --- | --- | --- |
| **Local password** | Protects Dens on this computer: backups, and seeing your keys | It never leaves this computer |
| **Den password** | Signing in to a den from another device | One per den. The den never sees it, so reusing one across dens is safe |
| **Recovery codes** | Signing in when you've forgotten your den password, or have no other device | Ten per den, and each works once. You can make new ones in **Manage** |
| **DM recovery key** | Reading your DMs if you lose every device | The same for every den on this computer. Keep it secret: it opens your DMs |

You'll find the last three again under the den's **Manage**, in **You in this
den**, and the local password in **Settings**, under **System**.

## Signing in from another computer

Every computer runs its own Dens. To use a den from a second one:

1. Install Dens there, open it, and choose **Add a den**.
2. Paste an invite to the den, even a used one, or the den's address. Choose
   **Sign in to my account there**.
3. Enter your username and den password.
4. Approve the sign-in from a device that's already in. Both screens show
   digits, and you type each one's into the other. That proves the new device
   is the one in front of you, and it hands over what the new device needs to
   read your DMs.

No other device at hand? Use a recovery code instead, then enter your DM
recovery key to read your DMs.

If a sign-in you didn't start asks for approval, refuse it, and change your den
password: whoever asked has it.

**Forgot your den password?** Choose **Forgot your password? Use a recovery
code** when signing in. You'll set a new password, and your other devices are
signed out.

## Direct messages

DMs are end-to-end encrypted: only you and the other person can read them, not
the den. To start one, open someone's profile and choose
**Message**.

Before your first message, you compare a **check code**: each of you reads out
half of it and types in the other's half. The den passes your keys between you,
so if it were tampered with, it could slip in keys of its own. The digits only
match if nobody did. Compare somewhere the den can't reach, best first:

1. In person.
2. On a voice or video call, where you know it's really them.
3. In another den that one of you owns. Not this one, even if it's yours.
4. On several other services where you know the accounts are really theirs.
5. In a den neither of you owns, which could change what you see there.

Be wary of "I'm @them over there, let's compare there": compare where you know
who you're talking to. If the digits don't match, someone may be in the middle,
so start the DM again.

## Leaving

Open the den's **Manage**, and choose **Leave den** at the bottom. You can come
back with a new invite, as yourself: join with your old username and den
password, and your history comes back with you, along with new recovery codes.

Leaving needs the den to answer. If Dens can't reach it, because its server
is gone for good, say, **Leave den** offers **Remove from this computer**
instead. That deletes your keys for the den on this computer, and the
messages and files it kept, without the den hearing of it: your account
stays there. If the den comes back, sign in again from **Add a den**, with its
address, your username and your den password.
