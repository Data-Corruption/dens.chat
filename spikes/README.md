# Platform spikes

Throwaway experiments that checked the platform-layer assumptions in
[docs/dev/design.md](../docs/dev/design.md) before M0. This is a separate Go
module, so the root module's tests, vet and hygiene checks don't see it. M0
deletes this directory once `internal/platform/host` exists; take code from it,
not dependencies on it.

Their findings are already in the design doc. This file records what was run
and what came back, so a result can be checked again.

## Linux: `linuxsvc`

A static binary installed as a templated system unit with the full hardening
set from the design doc, `LoadCredentialEncrypted=`, `StateDirectory=` and
`RuntimeDirectory=`. It opens a 0666 control socket authorized by
`SO_PEERCRED`, the client and media listeners, and low-priority children. A
client then runs as the recorded desktop user and as another local user.

```sh
# once: copy the images the driver expects
incus image copy images:fedora/44 local: --alias dens-spike/fedora-44   # and so on
./spikes/linuxsvc/matrix.sh
PRIVILEGED=true DISTROS="debian-12 ubuntu-noble" ./spikes/linuxsvc/matrix.sh
python3 spikes/summarize.py out/spikes/linux/<run>/<distro>/results/*.json
```

Run on 2026-09-26 against Fedora 43 (systemd 258), Fedora 44 (259), Debian 12
(252), Debian 13 (257), Ubuntu 24.04 (255), Ubuntu 26.04 (259) and Arch (262).
Results were the same on all seven:

- The unit starts with `Type=notify`, and `systemd-analyze security` rates it
  1.5 ("OK"). `NoNewPrivs` is 1, every capability set is empty, the seccomp
  filter is active, umask is 0077, `RLIMIT_CORE` is 0, `mlock` works and a
  writable-executable mapping is refused. `ProtectProc=invisible` leaves one
  visible process.
- The encrypted credential, kept inside the state directory, arrives in
  `$CREDENTIALS_DIRECTORY` (mode 0500, owned by the service account), and its
  fingerprint matches the key the installer encrypted. Other users can read
  neither the credential nor the state directory.
- The service can write only to its state, runtime and private temporary
  directories. Home directories, `/root`, `/etc/shadow` and `/proc/1` are
  refused.
- `SO_PEERCRED` identifies the client to the service and the service to the
  client. Another local user connects and is denied.
- No other user can bind the client port on IPv4 in any form: plain,
  `SO_REUSEADDR`, `SO_REUSEPORT` or wildcard. **Another user can bind `[::1]`
  on the same port when the service listens on IPv4 only;** binding both
  loopbacks closes it.
- **`net.Interfaces` fails under
  `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`** because it uses netlink.
  Adding `AF_NETLINK` fixes it.
- Nice values and scheduling policies are per thread. Setting nice 19 and
  `SCHED_IDLE` on the forking thread (locked to the goroutine and discarded
  afterwards) gives every thread of the child both. Both are allowed under
  `@system-service` and `RestrictRealtime`.
- `Restart=on-failure` restarts the service after `SIGKILL`; `systemctl stop`
  ends it cleanly in about 15 ms.
- Debian 13 has no `/etc/sysusers.d`. When a sysusers entry asks for a UID
  that's taken, sysusers picks another and still exits 0. `StateDirectory=`
  re-owns the root-created state directory and credential to the service
  account on start.

Not covered: SELinux (containers can't enforce it), TPM2 binding (containers
have no TPM), the Bazzite layout, Pion and a real ffmpeg.

Container caveats, which also apply to the lifecycle E2E harness:

- LXC adds `/run/systemd/system/service.d/zzz-lxc-service.conf` to every
  service in an unprivileged container. It sets `NoNewPrivileges=no`, empties
  `LoadCredential=` (which drops encrypted credentials too) and
  `ImportCredential=`, and turns off `ProtectProc`, `ProcSubset`,
  `ProtectControlGroups`, `ProtectKernelTunables` and `PrivateNetwork`. The
  unit starts but runs unhardened and without its key. `run.sh` masks the file
  and `matrix.sh` starts containers with `security.nesting=true`.
- systemd before 256 fails to create `/var/lib/systemd/credential.secret` in
  an unprivileged container (linking its temporary file fails with ENOENT), so
  Debian 12 and Ubuntu 24.04 need `PRIVILEGED=true`.

## Windows: `winsvc`

An elevated orchestrator installs `dens-spike-main` as an SCM service running
as `NT SERVICE\dens-spike-main` with `SERVICE_SID_TYPE_RESTRICTED`, required
privileges limited to `SeChangeNotifyPrivilege`, and restart-on-failure
recovery actions. It also sets up a protected-DACL state directory, a
machine-scope DPAPI data key and a Windows Error Reporting exclusion. A client
running as the non-elevated desktop user probes the service. The orchestrator
then kills the service three ways, stops it through the SCM and removes
everything.

```sh
./spikes/winsvc/run.sh          # from WSL; approve one UAC prompt
python3 spikes/summarize.py out/spikes/windows/<run>/*.json
```

Run on 2026-09-26 on Windows 11 Pro 24H2 (build 26100):

- The service token is the virtual account's SID (`S-1-5-80-…`),
  write-restricted, with restricted SIDs for the service SID, Everyone, the
  logon session and WRITE RESTRICTED. It has only `SeChangeNotifyPrivilege`
  and runs at High integrity. Its groups include `BUILTIN\Users`, so it can
  read anything the Users group can.
- Writes succeed only in the state directory and the service profile's temp
  folder. The `C:\ProgramData` root, `C:\Windows\Temp` and `C:\Users\Public`
  are all normally writable by Users, and all are refused, as are the state
  root, Program Files and the desktop user's profile. The hosts file is
  readable; the desktop user's profile is not.
- The DPAPI blob decrypts in the service, and its fingerprint matches the key
  the installer protected. The desktop user can decrypt any machine-scope blob
  it can read, so the file ACL is the only protection. The desktop user can't
  list the state directory or read the blob.
- `VirtualLock`, `net.Interfaces`, and the UDP and TCP listeners work. A child
  started with `IDLE_PRIORITY_CLASS` keeps class 0x40 and the restricted token.
- The control pipe's DACL grants SYSTEM and the service SID full access and the
  user `0x12019b`. The user can connect, including with a plain
  `GENERIC_READ | GENERIC_WRITE` open. `GetNamedPipeServerProcessId` matches
  the PID the SCM reports to the unprivileged user. Identification-level
  impersonation yields the user's SID without `SeImpersonatePrivilege`. The
  user can't add a server instance, and a pipe whose DACL omits the user
  refuses the connection. Granting the user `GENERIC_WRITE` instead lets it
  add server instances. `FILE_FLAG_FIRST_PIPE_INSTANCE` fails closed when the
  user claimed the name first.
- Another account can't bind `127.0.0.1` on the service's port, with or
  without `SO_EXCLUSIVEADDRUSE` on the service side, and with or without
  `SO_REUSEADDR` on its own. It can bind `0.0.0.0` on that port, but loopback
  connections still reach the specific `127.0.0.1` socket (the
  `bind-precedence` subcommand checks this). **It can bind `[::1]` unless the
  service holds it too.**
- An elevated administrator can't open the service process for termination
  without `SeDebugPrivilege`. `taskkill /F`, `Stop-Process -Force` and
  `TerminateProcess` after enabling the privilege all work, and the SCM
  restarts the service after each in about 1.3 s. A graceful SCM stop takes
  about 200 ms and exits with code 0.
- Uninstall removes the service, both directories and the WER exclusion.
