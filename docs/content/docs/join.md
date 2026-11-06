---
title: Join a den
weight: 3
---

A **den** is one community on Dens: its channels, its members and their DMs,
all on a computer run by one of them. To join one you need Dens
[installed](../install/), and an **invite** from someone there: a long code
starting with `dens1:`.

## Joining

1. Open Dens (`dens open`) and choose **Add a den**.
2. Paste the invite, and choose **Join as someone new**.
3. Pick a username, a display name and a password for this den. The username
   can't change, so pick one you'll keep.
4. Save your recovery codes and your DM recovery key somewhere safe, such as a
   password manager. The next section says what they're for.

{{% details title="Usernames, display names and profiles" closed="true" %}}

Your **username** is how people mention you: 2 to 32 characters, from
lowercase letters, numbers and `_`. Your **display name** is what people see,
and you can change it any time. You have a separate profile in each den, and
no den can link it to your others.

{{% /details %}}

{{% details title="Checking it's the den you meant" closed="true" %}}

Each den has a **Den ID**, which everyone in it sees the same. Comparing it
with someone already in the den tells you that you both joined the same one.

{{% /details %}}

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
4. Approve the sign-in from a device that's already in: type the digits each
   screen shows into the other.

No other device at hand? Use a recovery code instead, then enter your DM
recovery key to read your DMs.

If a sign-in you didn't start asks for approval, refuse it, and change your den
password: whoever asked has it.

{{% details title="Why your devices approve each other" closed="true" %}}

Typing each screen's digits into the other proves the new device is the one in
front of you, and it hands over what the new device needs to read your DMs.

{{% /details %}}

**Forgot your den password?** Choose **Forgot your password? Use a recovery
code** when signing in. You'll set a new password, and your other devices are
signed out.

## Your profile

Each den has your profile in it: your display name, a few lines about you, a
picture and a banner. Open it from your name at the foot of the den's channel
list.

{{% details title="Pictures, and when they move" closed="true" %}}

Drag a picture around its frame and zoom it to choose what shows; what the
file carries besides its pixels, such as where a photo was taken, stays on
your computer.

A GIF or a WebP that moves keeps moving. Dens on your computer makes it into a
GIF of at most 256 × 256 pixels for a picture, or 600 × 200 for a banner. All
of the animation goes in, so a long one can come out over the limit, 2 MB for a
picture and 4 MB for a banner, and Dens says so; a shorter clip fits.

**Animations**, under **Appearance** in the settings, chooses when moving
pictures and emoji play: **Always**, or **On hover**, only while your pointer
is over someone's message or name, and in a profile you open. It starts at On
hover if your system asks for less motion. On a phone, On hover plays them only
in profiles.

{{% /details %}}

## Compact messages

**Settings** → **Appearance** → **Message density** → **Compact** shows each
message as a line of its time, who sent it and the text, without pictures.
Each browser keeps its own choice, as the Android app does.

## Emoji

The smiley in the message box picks an emoji, or type a colon and two letters,
like `:th`, for a list of those that match. A den's own emoji, which its
moderators add, go by name between colons, like `:party_parrot:`.

{{% details title="More on emoji" closed="true" %}}

In the picker, the ones you use most come first, then the den's own, then the
usual ones by group, with a search over their names. ✋ at the top picks a skin
tone, which your browser remembers.

Write a den's emoji by its name and everyone sees the picture. In code, in a
link, or with a name the den doesn't have, it stays as you typed it. If a
moderator renames or deletes an emoji, the messages that used it show its name
instead.

In the list of matches, up and down choose, Enter or Tab puts one in, and Esc
closes it; on a phone, tap one. In a channel, `@` lists the people who can see
it, and for moderators and the owner, `@everyone`, which lets everyone who can
see the channel know.

A message of nothing but emoji, up to ten, shows them large, and the den's
moving ones play as **Animations** says. Dens on your computer keeps a copy of
every one of a den's emoji, so the den doesn't learn which ones you see, in a
DM or anywhere else.

The usual emoji are your system's, so they look the way they do everywhere else
on your computer or phone. A phone too old for a newer one shows a box.

{{% /details %}}

## Reactions

Point at a message and click the smiley beside its **⋯**, or choose **Add
reaction** from the **⋯** menu; on a phone, press and hold the message. Click a
reaction under a message to add yours or take it back.

{{% details title="More on reactions" closed="true" %}}

Reactions show under the message, each emoji with how many picked it, yours
highlighted. Point at one to see who reacted. **+** at the end of the row picks
another. A message holds up to 20 different emoji, and reacting doesn't notify
anyone.

On a phone, the six emoji you use most sit at the top of a message's menu, with
**+** for the rest. Tap a reaction to add or take back yours, and press and
hold it to see who reacted.

Moderators can take an emoji's reactions off a message in a channel. Reactions
in a DM are end-to-end encrypted, like its messages: the den sees that yours
changed, and when, but not what they are.

{{% /details %}}

## Polls

In a channel, **+** beside the message box has **Poll**. Write the question in
the box and 2 to 10 choices above it, pick its rules, and send it.

{{% details title="Its rules" closed="true" %}}

- **Members may pick more than one**, or only one choice each.
- **Runs for**: an hour to two weeks, a day at first. It can't be changed once
  the poll is sent, but you can end it early: **End now** on your poll.
- **Totals show**: **As votes come in**, or **After voting**, where each person
  sees the totals once they vote, and their vote is then final, so nobody moves
  to the winning side.
- **Anonymous voting**: **Yes**, so nobody sees who voted for what; **During
  the poll**, so it shows once the poll ends; or **No**, so it shows beside
  the totals.

{{% /details %}}

{{% details title="Voting, and who sees what" closed="true" %}}

A poll can't be edited, so nobody's vote ends up under a question they didn't
answer. Vote by picking a choice and **Vote**. Where the totals show as votes
come in, **Change vote** and **Remove vote** work until it ends. Where the poll
shows who voted for what, point at a choice's count, or tap it, to see who
picked it.

Anonymous voting keeps who voted for what from everyone in the den, its owner
and the person who asked included. The den itself still knows, since it counts
each person's vote once and lets them change it. DMs have no polls.

{{% /details %}}

## Spoilers

To send a photo or video as a spoiler, choose **Spoiler** on it in the message
box before you send it. Everyone sees it covered until they click or tap it.

{{% details title="More on spoilers" closed="true" %}}

For text, put the spoiler between two pairs of bars, like
`||the butler did it||`.

A photo stays a spoiler once it's sent, and covers itself again each time its
message shows. In a DM or a group DM only its members know it's a spoiler; in
a channel the den knows too. Someone on a version of Dens from before
spoilers sees it uncovered.

{{% /details %}}

## Direct messages

DMs are end-to-end encrypted: only you and the other person can read them, not
the den. To start one, open someone's profile and choose **Message**, and
write: it opens at once.

The den passes your keys between you, so if it were tampered with, it could
slip in keys of its own. To be sure it didn't, compare codes.

### Comparing codes

Open the lock beside the person's name in the DM, or **Compare codes** on their
profile. Read your code out to them, and type theirs as they read it out. If
both match, nobody sits between you, and a check shows beside their name.

Compare somewhere the den can't reach, best first:

1. In person.
2. On a voice or video call, where you know it's really them.
3. In another den that one of you owns. Not this one, even if it's yours:
   whoever tampered with it could change the digits you send each other
   there too, so a den can't vouch for itself.
4. On several other services where you know the accounts are really theirs.
5. In a den neither of you owns, which could change what you see there.

If the digits don't match, someone may be in the middle: don't trust the DM
until you've compared somewhere else.

{{% details title="Beware of “let's compare over there”" closed="true" %}}

Be wary of "I'm @them over there, let's compare there": a tampered den can
pretend to be someone elsewhere too, so compare where you know who you're
talking to. Hosting your own den narrows that risk, but nothing removes it.

{{% /details %}}

{{% details title="Your code, calls and the check" closed="true" %}}

Each of you has a code in each den, 20 digits, the same for everyone who
compares with you. Your own code is in the den's **Manage**, under **Your
code**. Comparing is up to you, any time, and the den never learns whether you
did. A check shows on your other devices too, within the hour.

Calls are end-to-end encrypted too, with the same codes: the lock beside a call
shows yours, and everyone in the call, to compare with right there. A
comparison holds wherever you meet them after, in DMs, groups and calls.

Whether the check shows beside people in lists is up to you, under
**Settings** → **Appearance** → **Verified status**: always, only while the
pointer is over them, which is how it starts, or not at all; on a phone, shown
or not, which is how it starts.

{{% /details %}}

{{% details title="When someone's keys change" closed="true" %}}

If someone's keys change, as when they reset their DM recovery key, the DM says
so where it happened, and their check goes. If you'd compared codes with
them, the DM waits until you compare again, or choose **Send anyway**.

A DM with someone whose Dens hasn't been updated waits until it is. DMs where
you both typed each other's check digits, as Dens once asked, go on as they
did.

{{% /details %}}

### Group DMs

A group DM is a DM of three to ten people, end-to-end encrypted as a DM is:
only its members can read it, not the den. Start one from **+ New group** above
your DMs, or from **Add people** on the profile beside a DM. Someone added
reads it from when they joined, not before, and someone removed, or who left,
reads nothing written after.

{{% details title="Who can change a group" closed="true" %}}

The group's members show beside it, where a DM shows the other person's
profile, each with their check or **Compare**. Anyone in it can add people,
rename it or leave. Whoever made it can remove people, and once they've left,
whoever has been in it longest can. Each change shows in the group as a line,
such as "Alice added Carol".

Each change is signed by the member who made it, and every member's Dens checks
it, so the den can't add anyone on its own. If a change doesn't check out, the
group says someone may be in the middle, and nothing more is sent there. If
someone's keys changed since you compared codes with them, the group waits, as
a DM does, until you compare again or choose **Send anyway**.

{{% /details %}}

{{% details title="Names, and what groups don't have" closed="true" %}}

**Add people** starts a group of the two of you and those you add, and leaves
the DM as it is. Give a group a name if you like; without one, it's named by its
members. Only its members ever see the name: the den keeps it sealed.

Group DMs notify you, and count as unread, as DMs do. They have no polls and no
@everyone, and no calls. A device whose Dens hasn't been updated yet doesn't
see your groups until it is; your other devices go on as before.

{{% /details %}}

### Disappearing messages

Anyone in a DM or a group DM can have its messages disappear 1 hour, 1 day, 1
week or 4 weeks after they're sent: pick a time under **Disappearing
messages**, on the profile beside a DM or below a group's members. When its
time comes, a message is gone from the den and from everyone's devices, with
its photos and files. Anyone in the conversation can still copy or screenshot
it before then.

{{% details title="More on disappearing messages" closed="true" %}}

The time applies to messages sent from then on, and a line in the conversation
says who set it, and when. It shows beside the lock at the top, and a small
clock beside each message that will disappear says when.

A message goes even from a device that's offline at the time. A device whose
Dens hasn't been updated yet only lets it go once it's back in touch with the
den.

{{% /details %}}

## Notifications and sounds

Dens can tell you about a DM, or a message that mentions your username, but
nothing notifies you until you turn it on: open **Settings**, then
**Notifications & sounds**, and turn on **Direct messages**, **Mentions**, or
both. Then the bell beside a den's name mutes the whole den, and the bell at
the top of a channel or DM mutes just that one.

A notification shows the start of the message, a DM's too, to anyone who can
see your screen, or your phone's lock screen. Turn off **Show the message** and
it says only who sent one. Spoilers never show.

{{% details title="Where they show" closed="true" %}}

- **A Dens tab, anywhere in your browser.** Turning one on asks the browser to
  let Dens show notifications; say yes. Clicking one brings the tab forward at
  the message. If you said no, allow notifications for `localhost` in the
  browser's site settings.
- **No Dens tab open.** Dens's helper, which starts when you sign in, shows them
  as your desktop's own, and clicking one opens Dens in your default browser at
  the message. On Windows, a click opens it straight away if that browser is
  still signed in to Dens; otherwise run `dens open`, or pick Dens from the
  Start menu.
- **Android.** The app shows them while it isn't open on that channel. If you
  didn't allow notifications when Android asked, allow them for Dens in
  Android's settings, under Apps.

Nothing shows for a channel Dens has open on your screen, and reading a channel
on any of your devices clears its notification everywhere. A busy DM is one
notification, with its newest message. The settings live in Dens on your
computer, so every browser you use it with shares them.

{{% /details %}}

{{% details title="@everyone" closed="true" %}}

Moderators and the owner can write `@everyone` in a channel, which notifies
everyone who can see it, as a mention does. If that's too much, turn off
**@everyone**, under **Mentions**: it stops the notifications and their sound,
though those messages still count among your mentions. Muting the den or the
channel silences it too.

{{% /details %}}

### Sounds

Dens plays a short sound for a DM or a mention, and in a call as someone joins
or leaves, starts or stops sharing their screen, and as your microphone turns
on and off. Each is on until you turn it off, under **Sounds** in the same
settings, where ▶ plays it and the slider sets how loud it is.

{{% details title="How they play" closed="true" %}}

Your microphone's sounds play whether you mute, push to talk, deafen, or a
moderator mutes you, and the call's play as you join or leave too.

- **A message makes one sound.** With a notification in your browser, Dens's
  sound plays and the browser's own stays quiet. Turning off **Direct
  messages** or **Mentions** turns off its sound too. If your browser won't let
  Dens show notifications and the helper isn't running, the sound plays alone,
  in the Dens tab you used last, unless that tab shows the channel or you
  muted it. With no Dens tab open, the notification your desktop shows plays
  your desktop's sound.
- **On Android**, a notification plays Android's own sound, which you change
  in Android's settings for Dens's notifications. The rest play through the
  call, on the earpiece, the speaker or Bluetooth, wherever you hear it.
- **Deafened**, you hear only your microphone's sounds.
- A browser lets a page play sound only once you've clicked in it, so a Dens tab
  you haven't touched since it opened plays none.

{{% /details %}}

{{% details title="Sounds of your own" closed="true" %}}

**Replace…** swaps a sound for a file of your own: audio that plays in your
browser, such as an MP3, Ogg or WAV file, of at most 1 MiB and 10 seconds.
**Reset** brings back Dens's. Your file stays in Dens on your computer, for
every browser you use it with; it never goes to a den, and a backup holds it.

{{% /details %}}

## The soundboard

A den can have a soundboard: short sounds its moderators add, which anyone in a
call plays for everyone in it. In a call, the pads button beside the call's
other buttons opens it. Search for a sound by name, and tap one to play it.

{{% details title="How it plays" closed="true" %}}

The pads button is at the foot of the den's channel list, on your home page,
and in the bar at the top of a channel on a phone.

- **Everyone in the call hears it at once,** through the call, as they hear
  you, and someone joining partway hears the rest. Your ring lights up while
  yours plays, even if you're muted.
- **One plays at a time.** While one plays, or for a few seconds after your
  last one ends, the sounds wait, and say so. The den's owner sets how long
  sounds may be and how long you wait.
- **The slider** sets how loud the soundboard plays for you alone, and at 0
  you don't hear it at all. Deafened, you hear nothing of it either, and its
  sounds wait until you undeafen.
- **A long sound can't be stopped**, except by the person who played it
  leaving the call. Turn the slider down if you'd rather not hear it.

{{% /details %}}

Moderators and the owner add sounds from the den's **Manage**, under
**Soundboard**; see [Host a den](../host/#running-the-den).

## Leaving

Open the den's **Manage**, from the den's name at the top of its channel list,
and choose **Leave den** at the bottom. You can come back with a new invite, as
yourself: join with your old username and den password.

{{% details title="Coming back, and a den that's gone" closed="true" %}}

Coming back, your history comes back with you, along with new recovery codes.

Leaving needs the den to answer. If Dens can't reach it, because its server
is gone for good, say, **Leave den** offers **Remove from this computer**
instead. That deletes your keys for the den on this computer, and the
messages and files it kept, without the den hearing of it: your account
stays there. If the den comes back, sign in again from **Add a den**, with its
address, your username and your den password.

{{% /details %}}
