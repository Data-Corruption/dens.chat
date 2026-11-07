---
title: Hosting
weight: 3
aliases:
  - /docs/host/
  - /docs/server/
---

Dens on Linux or Windows can host one den; the Flatpak and the Android app
can't. The den lives on your computer, and members connect to it over the
internet. You need:

- **A computer that stays on.** The den is only up while it runs. A normal PC
  is enough, or [a server](#on-a-server).
- **Upload speed.** A new den's limits need about 20 Mbps of upload when
  every call is full; you can lower or raise them.
- **A domain name**, such as `den.example.com`. Any registrar sells them,
  usually for $10 to $20 a year.
- **Your router's settings**, to forward four ports to this computer.

Besides Dens, a den needs only Caddy, a small web server that gets it a free
HTTPS certificate. Members' Dens finds the den again by itself whenever it
moves or comes back.

{{% steps %}}

### Turn hosting on

In **Settings**, under **System**, turn on **Host a den on this computer**. It
asks for your local password, since it opens Dens to the internet. If another
program already uses one of the den's ports, the setting says which, and how
to give the den another.

### Create the den

The home page now shows **Create your den**: give it a name, its public
address (`https://` and your domain), and your owner account. This works
before the steps below, since Dens reaches its own den directly.

### Point your domain at your home

In your domain's DNS settings, add an `A` record for your den's name, pointing
at your home's public IP address (search "what is my IP" from this computer).
Add an `AAAA` record if your connection has an IPv6 address too. A home
address can change; many routers and DNS providers offer **dynamic DNS**,
which keeps the record up to date.

### Set up Caddy

Use your own domain in place of `den.example.com`.

{{< tabs >}}
{{< tab name="Linux" >}}
Install it from your distribution (`apt install caddy`, `dnf install caddy`,
`pacman -S caddy`; on Bazzite, `rpm-ostree install caddy` and a restart). Then:

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
make a folder `C:\Caddy`, and put `caddy.exe` in it. Then, in Terminal
(Admin):

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
firewall.
{{< /tab >}}
{{< /tabs >}}

### Forward the ports

In your router's settings, often under **port forwarding**, forward these to
this computer, and reserve this computer's local address (a **DHCP
reservation**) so the forwarding keeps pointing at it:

| Port | Protocol | For |
| --- | --- | --- |
| 80 | TCP | Caddy getting its certificate |
| 443 | TCP | Members connecting |
| 7881 | UDP | Voice and screen share |
| 7882 | TCP | Voice and screen share, where UDP is blocked |

Some internet providers share one public address among many customers
(**CGNAT**), and then forwarding can't work. A small rented server with an
address of its own can host the den instead.

### Invite someone, and test a call

In the den's **Manage**, under **Invites**, create one and send it to a friend.
Your own calls work before any port is forwarded, so the test is a call from
outside your home: your friend, or a laptop on your phone's hotspot. Calls go
straight to the address your domain points at, so a tunnel or CDN that carries
your den's website can't carry its calls.

{{% /steps %}}

## Running the den

Everything is in the den's **Manage**: click the den's name at the top of its
channel list. Roles, invites, channels, limits and the soundboard work as
you'd expect; what's different:

- **Invites are the only way in.** Each expires and works a set number of
  times. Dens keeps no IP addresses, so someone banned can come back under a
  new name with a new invite: revoke the one they used.
- **Staff act only on people of a lower rank.** The owner promotes
  moderators from **⋯** on a profile, and sets the den's limits.
- **Limits** cap files, storage, and how big calls and screen shares get; the
  calls section shows the upload they need when full. Lower them if calls
  stutter.
- **Auto-delete** deletes messages older than a number of days, in channels
  and DMs, and members see the setting. A [backup](../backups/) keeps what it
  held until you delete the backup.
- **Calls are end-to-end encrypted.** Neither you nor anyone holding the
  computer can hear or watch them, and a member whose Dens is too old to seal
  them can't join one until they update.
- **A new address.** Change it in **General**, and keep the old name pointing
  at the den for a while, so members who were offline find the new one.
- **Updates.** Your den updates along with the Dens that hosts it. If you'd
  rather keep them apart, host the den from [an install of its
  own](#a-second-den).

## On a server

A den can live on a machine that stays on, such as a home server or a small
rented one, which needs no desktop or browser: everything on it is a command,
and you keep running the den from your own computer, as its owner. Install
Dens there over SSH [as on any Linux](../install/linux/); on a rented server,
the provider's firewall stands in for the router.

{{% details title="If the server has only root" closed="true" %}}

Make an account for yourself first, and name it when you install: Dens's
commands answer that one account, and it can't be root. Then run this page's
`sudo` commands as root, and the others as that account.

```sh
adduser alice
curl -fsSLO https://releases.dens.chat/install.sh
sh install.sh --user alice
```

In a Proxmox or Incus container, use Debian 13 with **nesting** on. The first
time, the installer stops and says why: LXC adds a setting that turns off the
locks Dens runs under. Switch it off, as root, and run the installer again:

```sh
mkdir -p /etc/systemd/system/service.d
ln -s /dev/null /etc/systemd/system/service.d/zzz-lxc-service.conf
systemctl daemon-reload
```

{{% /details %}}

**Moving a den there.** On the computer that hosts it now, in **Settings**
under **System**, choose **Move the den to another computer**: it turns
hosting off and downloads the den's **export**, a file that holds only the
den, locked with your local password. Members can't reach the den until the
server hosts it, so do the rest in one go:

```sh
scp dens-main-den-20261023-153000.backup alice@server.example:
ssh alice@server.example
sudo dens restore dens-main-den-20261023-153000.backup
dens den hosting on
```

Set Caddy up on the server as above, and move the four port forwards to it,
or on a rented server, point your domain's record at it. Then, back on your
computer, **Hosting** in the settings offers **Remove from this computer**,
which checks that the server serves the den before deleting your copy. From
then on your Dens reaches the den like any member's.

**Starting a new den there** instead:

```sh
dens password
dens den hosting on
dens den create "My Den" https://den.example.com
```

The last prints the den's **owner invite**. Paste it into **Add a den** on your
own computer, and you join as the den's owner.

{{% details title="If your computer can't reach the den at home" closed="true" %}}

Some routers can't reach their own public address from inside the home.
Give your computer a **hosts entry**, a line that points your den's name at
the server's local address, such as `192.168.1.20`:

```sh
echo '192.168.1.20 den.example.com' | sudo tee -a /etc/hosts
```

On Windows, in Terminal (Admin):

```powershell
Add-Content C:\Windows\System32\drivers\etc\hosts "`n192.168.1.20 den.example.com"
```

{{% /details %}}

### From the command line

- `dens status` says whether the den serves, and why not when it can't, and
  when a new version is out.
- `dens den hosting off` and `on` turn hosting off and on.
- `dens password` changes the local password, given the current one.
- `dens backup` writes a backup into the folder you're in; copy it off the
  server.
- `dens den owner-invite` hands the den to whoever joins with the invite it
  prints ([Backups and moving](../backups/#handing-a-den-to-someone-else)).
- `dens logs --level debug`, then `dens logs` after the problem happens again,
  saves the logs to send ([Sending logs](../usage/#sending-logs)).
- `sudo dens update` updates; members reconnect by themselves.

## A second den

One install hosts one den. A second den on the same computer is a second
**instance**: run the installer with `--instance` and a name, and ports of
its own (`--client-port`, `--den-port`, `--media-udp-port` and
`--media-tcp-port`; on Windows, `-Instance`, `-ClientPort` and so on, given
[as its guide shows](../install/windows/)), turn hosting on in its settings,
and give Caddy a second site.
