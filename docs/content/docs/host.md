---
title: Host a den
weight: 3
---

Dens, installed on Linux or Windows, can also host one den; the Flatpak can't.
The den lives on your computer, and members connect to it over the internet. This guide covers the router and
domain parts too, assuming you've never done either.

## What you need

- **A computer that stays on.** The den is only up while it runs. A normal PC
  is enough, or a server: [Host on a server](../server/) sets one up, and
  moves a den you host now onto it.
- **Upload speed.** Messages go out once to every member online, and calls and
  screen shares once to every member in them. A new den's limits need about
  20 Mbps of upload when every call is full; you can lower or raise them.
- **A domain name**, such as `den.example.com`, which members connect to. Any
  registrar sells them, usually for $10 to $20 a year.
- **Your router's settings**, to forward four ports to this computer.

Dens is one program with its database and media tools built in, so besides
Dens, a den needs only Caddy, a small web server that the steps below set up.

{{% steps %}}

### Turn hosting on

[Install](../install/) Dens if you haven't, and open it (`dens open`). In
**Settings**, under **System**, turn on **Host a den on this computer**. It
asks for your local password, since it opens Dens to the internet: the den's
own port, which Caddy passes connections on to, and the ports calls use.

If another program already uses one of those ports, the setting says which,
and how to give the den another: run the installer again with the matching
option, such as `--den-port` (`-DenPort` on Windows).

You can turn hosting off there too. Members can't reach the den until you turn
it back on, and nothing in it is lost.

### Create the den

The home page now shows **Create your den**: give it a name, its public
address (`https://` and your domain), and your owner account, with a username,
a display name and a den password.

This works before the steps below, since Dens reaches its own den directly.

### Point your domain at your home

In your domain's DNS settings, add an `A` record for your den's name, pointing
at your home's public IP address (searching "what is my IP" from this computer
tells you). If your connection has an IPv6 address too, add an `AAAA` record
for it.

A home connection's address can change. Many routers and DNS providers offer
**dynamic DNS**, which keeps the record up to date for you.

### Set up Caddy

Caddy gets your den a free HTTPS certificate, and passes connections on to
Dens. Use your own domain in place of `den.example.com`.

{{< tabs >}}
{{< tab name="Linux" >}}
Install it from your distribution:

| Distribution | Command |
| --- | --- |
| Debian, Ubuntu, Mint, Pop!\_OS | `sudo apt install caddy` |
| Fedora | `sudo dnf install caddy` |
| Arch, CachyOS | `sudo pacman -S caddy` |
| Bazzite | `rpm-ostree install caddy`, then restart |

Then give it your den, and start it:

```sh
sudo tee /etc/caddy/Caddyfile <<'EOF'
den.example.com {
	reverse_proxy 127.0.0.1:8485
}
EOF
sudo systemctl enable caddy
sudo systemctl restart caddy
```

If your system runs a firewall, let the den's ports through it. Fedora and
Bazzite do:

```sh
sudo firewall-cmd --permanent --add-service=http --add-service=https
sudo firewall-cmd --permanent --add-port=7881/udp --add-port=7882/tcp
sudo firewall-cmd --reload
```
{{< /tab >}}
{{< tab name="Windows" >}}
Download Caddy for Windows from [caddyserver.com](https://caddyserver.com/download),
make a folder `C:\Caddy`, and put `caddy.exe` in it. Then, in an admin
terminal:

```powershell
@"
den.example.com {
	reverse_proxy 127.0.0.1:8485
}
"@ | Set-Content C:\Caddy\Caddyfile
New-Service -Name caddy -BinaryPathName '"C:\Caddy\caddy.exe" run --config C:\Caddy\Caddyfile --adapter caddyfile' -StartupType Automatic
Start-Service caddy
New-NetFirewallRule -DisplayName Caddy -Direction Inbound -Program C:\Caddy\caddy.exe -Action Allow
```

The Dens installer already let the den's call ports through the Windows
firewall. Nothing answers on them until you turn hosting on.
{{< /tab >}}
{{< /tabs >}}

### Forward the ports

In your router's settings, often under **port forwarding**, forward these to
this computer:

| Port | Protocol | For |
| --- | --- | --- |
| 80 | TCP | Caddy getting its certificate |
| 443 | TCP | Members connecting |
| 7881 | UDP | Voice and screen share |
| 7882 | TCP | Voice and screen share, where UDP is blocked |

Routers differ too much to show here: search for your router's model and "port
forwarding". While you're there, reserve this computer's local address (often
called a **DHCP reservation**), so the forwarding keeps pointing at it.

Some internet providers share one public address among many customers (called
**CGNAT**), and then forwarding can't work. A small rented server (a VPS) with
an address of its own can host the den instead ([Host on a server](../server/)).

### Invite someone, and test a call

In Dens, open the den's **Manage**, then **Invites**, and create one. Send it to
a friend.

Your own calls work before any port is forwarded, so test from outside your
home: have your friend join a call with you, or join from a laptop on your
phone's hotspot. Calls go straight to the address your domain points at, so a
tunnel or CDN service that carries your den's website can't carry its calls.

{{% /steps %}}

## Running the den

Everything here is in the den's **Manage**, under **This den**.

- **Roles.** Members read and post, DM, call and upload. **Moderators** also
  make invites, remove and ban members, delete members' messages, manage
  channels, and see staff-only channels. The **owner** also picks moderators
  and sets the den's limits. Staff act only on people of a lower rank.
- **Invites** are the only way in. Each one expires, and works a set number of
  times.
- **Removing and banning.** Removing someone signs them out everywhere. A ban
  also keeps their username out, and can delete their recent messages. Dens
  keeps no IP addresses, so someone banned can come back under a new name with
  a new invite: revoke the invite they used.
- **Limits** cap file sizes and storage, and how big calls and screen shares
  get. The calls section shows the upload they need when full; lower them if
  calls stutter.
- **Auto-delete**, off unless you turn it on, deletes messages older than a
  number of days, in channels and DMs. Members see the setting. A
  [backup](../backups/) keeps what it held until you delete the backup.
- **A new address.** Change it in **General**, and keep the old name pointing
  at the den for a while, so members who were offline find the new one.

## What hosting costs you

Mostly upload. Dens keeps it small: members pick up where they left off instead
of reloading everything, who's online goes out in batches, and pictures go out
as previews until someone opens them. A den is sized for about a thousand
members, 500 of them online at once.

Your den updates along with the Dens that hosts it: `dens update` updates both.
If you'd rather keep them apart, host the den from an install of its own.

One install hosts one den. A second den on the same computer is a second
**instance**: run the installer again with `--instance` and a name, and ports
of its own (`--client-port`, `--den-port`, `--media-udp-port` and
`--media-tcp-port`; on Windows, `-Instance`, `-ClientPort` and so on), turn
hosting on in its settings, and give Caddy a second site.
