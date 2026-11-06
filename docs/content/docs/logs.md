---
title: Sending logs
weight: 8
---

When something goes wrong, such as a call that keeps dropping, Dens's logs say
what it did and when, which helps whoever looks into it.

{{% steps %}}

### Turn them up

In **Settings**, under **System**, set **Log level** to **debug**.

{{% details title="What changes at debug" closed="true" %}}

Dens logs the most, calls in detail: each connection's ups and downs, and
every 10 seconds how a call's connection is doing. It takes hold at once, and
stays until you set it back.

{{% /details %}}

### Let it happen again

The logs hold only what happened after you turned them up.

### Download them

Under the level, **Download the newest logs** saves the newest 32 MiB of them
as one text file. Send that.

{{% /steps %}}

Then set the level back to **warn**: at debug the logs grow faster.

{{% details title="What's in them" closed="true" %}}

No messages, no keys or passwords, and no network addresses: not your
computer's, anyone else's, or your dens'. They do name your dens, by name and
by the den ID **Manage** shows, and the channels and members involved, by
number. They're plain text, so you can read them before you send them.

{{% /details %}}

{{% details title="For a call that drops: the den's logs" closed="true" %}}

The den's logs help too, from the computer that hosts it. Its owner gets them
the same way, or on a server with `dens logs --level debug` before, and
`dens logs` after, which saves them in the folder you're in
([Running the den on the server](../server/#running-the-den-on-the-server)).

{{% /details %}}
