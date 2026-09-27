#!/bin/sh
# Runs as root inside a disposable container. Installs the spike binary as a
# hardened templated system unit, exercises it as two desktop users, and writes
# JSON reports to /root/results. Everything else goes to stdout.
set -u
R=/root/results
mkdir -p "$R"

step() { printf '\n== %s\n' "$*"; }

wait_active() {
	# wait_active UNIT OLD_PID: until UNIT is active with a main PID other than OLD_PID
	i=0
	while [ "$i" -lt 60 ]; do
		state=$(systemctl show -p ActiveState --value "$1")
		pid=$(systemctl show -p MainPID --value "$1")
		if [ "$state" = active ] && [ "$pid" != 0 ] && [ "$pid" != "$2" ]; then
			return 0
		fi
		sleep 0.5
		i=$((i + 1))
	done
	return 1
}

. /etc/os-release
step "distro"
echo "$PRETTY_NAME"
systemctl --version | head -1
uname -r

step "container workaround"
# LXC injects a drop-in into every service that disables NoNewPrivileges,
# credentials, ProtectProc, ProcSubset, ProtectControlGroups and
# ProtectKernelTunables. Mask it so the unit below runs as it would on a host;
# the container is started with security.nesting=true so those settings work.
if [ -e /run/systemd/system/service.d/zzz-lxc-service.conf ]; then
	mkdir -p /etc/systemd/system/service.d
	ln -sf /dev/null /etc/systemd/system/service.d/zzz-lxc-service.conf
	systemctl daemon-reload
	echo "masked zzz-lxc-service.conf"
fi
systemd-detect-virt

step "users"
useradd -m alice
useradd -m mallory
printf 'alice private note\n' > /home/alice/secret.txt
chown alice: /home/alice/secret.txt
chmod 600 /home/alice/secret.txt
alice_uid=$(id -u alice)
echo "alice uid=$alice_uid"

step "binary"
install -m 0755 -o root -g root /root/dens-spike /usr/local/bin/dens-spike
ls -l /usr/local/bin/dens-spike

step "sysusers with a fixed UID"
# Some distros ship only /usr/lib/sysusers.d.
install -d -m 0755 /etc/sysusers.d
printf 'u dens-spike-main 873 "Dens spike (main)" - -\n' > /etc/sysusers.d/dens-spike-main.conf
systemd-sysusers /etc/sysusers.d/dens-spike-main.conf
echo "exit=$?"
id dens-spike-main

step "sysusers when the fixed UID is taken"
useradd -r -u 874 -M squatter874
printf 'u dens-spike-collide 874 "collision test" - -\n' > /etc/sysusers.d/dens-spike-collide.conf
systemd-sysusers /etc/sysusers.d/dens-spike-collide.conf
echo "exit=$?"
id dens-spike-collide

step "state directory and encrypted credential"
install -d -m 0755 -o root -g root /var/lib/dens-spike
install -d -m 0700 -o root -g root /var/lib/dens-spike/main
old_umask=$(umask)
umask 077
head -c 32 /dev/urandom > /run/dens-spike-key
echo "key sha256 prefix: $(sha256sum /run/dens-spike-key | cut -c1-16)"
systemd-creds has-tpm2 > /dev/null 2>&1
echo "has-tpm2 exit=$?"
systemd-creds encrypt --name=datakey /run/dens-spike-key /var/lib/dens-spike/main/datakey.cred
echo "encrypt exit=$?"
rm -f /run/dens-spike-key
umask "$old_umask"
ls -la /var/lib/dens-spike/main

step "unit"
cat > /etc/systemd/system/dens-spike@.service <<EOF
[Unit]
Description=Dens spike (%i)
After=network.target

[Service]
Type=notify
User=dens-spike-%i
Group=dens-spike-%i
ExecStart=/usr/local/bin/dens-spike service -allowed-uid $alice_uid -desktop-home /home/alice
LoadCredentialEncrypted=datakey:/var/lib/dens-spike/%i/datakey.cred
StateDirectory=dens-spike/%i
StateDirectoryMode=0700
RuntimeDirectory=dens-spike/%i
RuntimeDirectoryMode=0755
UMask=0077
Restart=on-failure
RestartSec=1
TimeoutStopSec=15
LimitCORE=0
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
ProcSubset=pid
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RemoveIPC=yes
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload

step "start"
systemctl start dens-spike@main
echo "start exit=$?"
systemctl show -p ActiveState -p SubState -p MainPID -p NRestarts dens-spike@main
journalctl -u dens-spike@main --no-pager -o cat | tail -n 30
ls -ld /var/lib/dens-spike/main /run/dens-spike /run/dens-spike/main
ls -la /var/lib/dens-spike/main /run/dens-spike/main

step "client as alice (the recorded desktop user)"
runuser -u alice -- /usr/local/bin/dens-spike client > "$R/alice.json"
echo "exit=$?"

step "client as mallory (another local user)"
runuser -u mallory -- /usr/local/bin/dens-spike client > "$R/mallory.json"
echo "exit=$?"

step "systemd-analyze security"
systemd-analyze security dens-spike@main.service --no-pager 2>&1 | tail -n 3

step "interface enumeration under RestrictAddressFamilies"
for families in "AF_INET AF_INET6 AF_UNIX" "AF_INET AF_INET6 AF_UNIX AF_NETLINK"; do
	printf '%s: ' "$families"
	systemd-run --wait --pipe --quiet -p DynamicUser=yes -p "RestrictAddressFamilies=$families" \
		/usr/local/bin/dens-spike probe-net 2>&1
done

step "restart after a crash"
old_pid=$(systemctl show -p MainPID --value dens-spike@main)
systemctl kill -s SIGKILL dens-spike@main
if wait_active dens-spike@main "$old_pid"; then echo "restarted"; else echo "did not restart"; fi
systemctl show -p MainPID -p NRestarts -p ActiveState dens-spike@main

step "graceful stop"
start=$(date +%s%N)
systemctl stop dens-spike@main
end=$(date +%s%N)
echo "stop took $(( (end - start) / 1000000 )) ms"
journalctl -u dens-spike@main --no-pager -o cat | grep -E 'stop requested|stopped cleanly' | tail -n 2
ls -l /var/lib/dens-spike/main/stopped-cleanly
