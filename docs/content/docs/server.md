---
title: Host on a server
weight: 4
---

A den can live on a machine that stays on, such as a home server, a mini PC
in a cupboard, or a small rented server, instead of on the computer you use
every day. This guide puts Dens on a Linux server you reach over SSH, and
moves a den you host now onto it, or
[starts a new one there](#starting-a-new-den-on-the-server). The server needs
no desktop or browser: everything on it is a command, and you keep running
the den from your own computer, as its owner.

## What you need

- **A Linux machine that stays on**, running one of the distributions
  [Dens installs on](../install/#what-it-runs-on). Debian 13 is a good pick
  for a server. A container on Proxmox or Incus works too.
- **SSH access** to it, with an account that can use `sudo`, or as root.
- **Your domain and your router**, as in [Host a den](../host/). On a rented
  server, the provider's firewall stands in for the router.

{{% steps %}}

### Install Dens on the server

Over SSH, as your own account, download the installer and run it with
`sudo`, as on any Linux ([Install](../install/)):

```sh
curl -fsSLO https://releases.dens.chat/install.sh
sudo sh install.sh
```

If the server has only root, as a fresh container often does, make an
account for yourself first, and name it when you install: Dens's commands
answer that one account, and it can't be root.

```sh
adduser alice
sh install.sh --user alice
```

From then on, run this guide's `sudo` commands as root, without `sudo`, and
the others as that account: log in over SSH as it, or switch to it with
`su - alice`.

{{% details title="In a Proxmox or Incus container" closed="true" %}}

Use Debian 13, with **nesting** on: on Proxmox, it's under the container's
**Options**, in **Features**, and on Incus, it's
`incus config set NAME security.nesting=true`.

The first time, the installer stops and says why: LXC adds a setting to
every service in a container that turns off the locks Dens runs under,
including the one that keeps its key. Switch that setting off, as root, and
run the installer again:

```sh
mkdir -p /etc/systemd/system/service.d
ln -s /dev/null /etc/systemd/system/service.d/zzz-lxc-service.conf
systemctl daemon-reload
```

{{% /details %}}

### Export the den

On the computer that hosts the den now, open Dens's **Settings**, and under
**System**, in **Hosting**, choose **Move the den to another computer**. It
asks your local password, turns hosting off, and your browser downloads the
den's **export**: a file named like `dens-main-den-20261023-153000.backup`.

Or, from a terminal on that computer, as yourself:

```sh
dens den hosting off
dens den export
```

The export only holds the den: its members, messages and files, and the key
that proves it's your den. None of your client data goes with it: the dens
you've joined from this computer, your keys there and your settings stay on
it. Like a backup, the export is locked with your local password.

Members can't reach the den from here on until the server hosts it, so do the
next steps in one go.

### Restore it on the server

Copy the file to the server, from your computer:

```sh
scp dens-main-den-20261023-153000.backup alice@server.example:
```

Then, on the server:

```sh
sudo dens restore dens-main-den-20261023-153000.backup
```

It asks the export's local password, the one from your computer, which is
the server's local password from now on.

### Turn hosting on

On the server, as your account:

```sh
dens den hosting on
```

It asks the local password, and says where the den is served:

```text
Hosting is on, and My Den serves at https://den.example.com.
Caddy forwards the den's address to 127.0.0.1:8485, and the router forwards
port 7881 (UDP) and port 7882 (TCP) to this computer, for calls.
```

### Move Caddy and the ports

Set Caddy up on the server, as in [Host a den](../host/#set-up-caddy), in its
Linux tab. Then point the den's traffic at the server:

- **At home**, your public address doesn't change, so neither does your
  domain. In your router's settings, move the four port forwards to the
  server's local address, and reserve that address for it.
- **On a rented server**, change your domain's `A` record (and `AAAA`, if it
  has one) to the server's address, and let ports 80, 443, 7881 (UDP) and 7882
  (TCP) through the provider's firewall.

Stop Caddy on your own computer, which has nothing to serve now:

{{< tabs >}}
{{< tab name="Linux" >}}
```sh
sudo systemctl disable --now caddy
```
{{< /tab >}}
{{< tab name="Windows" >}}
In an admin terminal:

```powershell
Stop-Service caddy
Set-Service caddy -StartupType Disabled
```
{{< /tab >}}
{{< /tabs >}}

Members' Dens reconnect by themselves once the den answers at its address.
A change to a domain's record can take a while to reach everyone.

### Remove the den from your computer

Your computer still has its copy. Once the server serves the den, open
**Hosting** again and choose **Remove from this computer**, or run:

```sh
dens den remove
```

It asks your local password, and checks first that hosting is off on this
computer, that you exported the den since it was last on, and that the den
answers at its address from here, as itself. Then it deletes this computer's
copy, and Dens reaches the den at its address like any member's does, with
your account, your history and your DMs.

{{% /steps %}}

## Starting a new den on the server

To start a den on the server rather than move one there, install Dens on it
as above. Then, over SSH, as your account:

{{% steps %}}

### Set the local password

```sh
dens password
```

It asks for a new local password, twice. As on your own computer, it
protects Dens on the server: hosting, backups and the den's commands ask for
it, and a backup of the server opens only with it.

### Turn hosting on

```sh
dens den hosting on
```

It asks the local password, and says where Caddy and the router send the
den. Set Caddy up on the server and forward the ports to it, as in
[Host a den](../host/).

### Create the den

Give it a name, and the address members will reach it at:

```sh
dens den create "My Den" https://den.example.com
```

It asks the local password, and prints the den's **owner invite**:

```text
Created My Den, which members reach at https://den.example.com. Its owner invite:

dens1:...

Whoever pastes it into Add a den, on their own Dens page, becomes the den's owner.
It works once, within the hour; make another with: dens den owner-invite
```

### Become its owner

On your own computer, in Dens, choose **Add a den**, paste the invite, and
choose a username and a den password. You join as the den's owner, and run it
from there, from its **Manage**: invite your members, and set it up as you
like.

{{% /steps %}}

## Reaching the den from home

If the den is on a server at home and your computer can't reach it at its
address, as when **Remove from this computer** says the den doesn't answer
there, or its owner invite won't go through, your router may not reach its own
public address from inside your home. Some routers can, under a setting called
**NAT loopback** or **hairpin NAT**. If yours can't, give your computer a
**hosts entry**: a line that points your den's name at the server's local
address, such as `192.168.1.20`, on this computer alone.

{{< tabs >}}
{{< tab name="Linux" >}}
```sh
echo '192.168.1.20 den.example.com' | sudo tee -a /etc/hosts
```
{{< /tab >}}
{{< tab name="Windows" >}}
In an admin terminal:

```powershell
Add-Content C:\Windows\System32\drivers\etc\hosts "`n192.168.1.20 den.example.com"
```
{{< /tab >}}
{{< /tabs >}}

Calls go to whatever address the name points at, so with the hosts entry,
yours stay on your home network. Do the same on any other computer in your
home that's in the den.

## Running the den on the server

- **The local password.** `dens password` changes it, given the current one.
  A backup made before still opens with the password it was made under.
- **Hosting.** `dens den hosting off` and `dens den hosting on` turn it off and
  on, and `dens status` says whether the den serves, and why not when it
  can't. Turning it off asks first, since members can't reach the den until
  it's back on.
- **Running the den** happens from your own computer, as its owner: its
  **Manage** is where it always was.
- **Handing it on.** `dens den owner-invite`, on the server, makes a one-time
  invite that makes whoever joins with it the owner ([Backups and moving](../backups/#handing-a-den-to-someone-else)).
- **Updates.** `dens status` says when a new version is out. Update with
  `sudo dens update`; members reconnect by themselves once the server's back.
- **Backups.** `dens backup`, as your account, writes a backup of the server
  into the folder you're in. Copy it off the server, to keep it somewhere
  else. It holds the den, as the export did.
