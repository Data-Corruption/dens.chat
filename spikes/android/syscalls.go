package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"golang.org/x/sys/unix"
)

// syscallNames are the calls the report tries, each in a child: those Dens
// and Go's runtime make, or might, and those Android is known to refuse
// apps. Each child exits 0 with what it saw, or 1 with the call's error; a
// call Android's seccomp filter refuses kills the child with SIGSYS.
var syscallNames = []string{
	"setpriority", "sched_setattr", "sched_setaffinity", "ioprio_set",
	"mlock", "madvise_dontdump", "memfd_create",
	"faccessat2", "lookpath", "statx", "openat2", "close_range",
	"flock", "ofd_lock", "fallocate", "copy_file_range", "splice", "sendfile",
	"pidfd_open", "pidfd_send_signal", "waitid_pidfd", "clone3",
	"pdeathsig", "setsid", "epoll_pwait2", "inotify", "getrandom",
	"membarrier", "rseq", "landlock", "io_uring_setup", "netlink_getlink",
}

func syscallChild(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: aprobe syscall NAME")
	}
	r := func(err error) error {
		if err == nil {
			fmt.Println("allowed")
		}
		return err
	}
	switch args[0] {
	case "noop":
		return nil
	case "sched":
		raw, err := unix.Getpriority(unix.PRIO_PROCESS, 0)
		if err != nil {
			return err
		}
		attr, err := unix.SchedGetAttr(0, 0)
		if err != nil {
			return err
		}
		fmt.Printf("nice %d, policy %d", 20-raw, attr.Policy)
		return nil
	case "startlow":
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "syscall", "sched")
		out, err := startAndOutput(cmd)
		if err != nil {
			return err
		}
		fmt.Printf("the child saw %s", out)
		return nil
	case "setpriority":
		return r(unix.Setpriority(unix.PRIO_PROCESS, 0, 19))
	case "sched_setattr":
		return r(unix.SchedSetAttr(0, &unix.SchedAttr{Size: unix.SizeofSchedAttr, Policy: unix.SCHED_IDLE}, 0))
	case "sched_setaffinity":
		var set unix.CPUSet
		if err := unix.SchedGetaffinity(0, &set); err != nil {
			return err
		}
		return r(unix.SchedSetaffinity(0, &set))
	case "ioprio_set":
		const whoProcess, classIdle = 1, 3
		return r(errnoErr(unix.Syscall(unix.SYS_IOPRIO_SET, whoProcess, 0, classIdle<<13)))
	case "mlock", "madvise_dontdump":
		mem, err := unix.Mmap(-1, 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
		if err != nil {
			return err
		}
		if args[0] == "mlock" {
			return r(unix.Mlock(mem))
		}
		return r(unix.Madvise(mem, unix.MADV_DONTDUMP))
	case "memfd_create":
		_, err := unix.MemfdCreate("aprobe", unix.MFD_CLOEXEC)
		return r(err)
	case "faccessat2":
		return r(unix.Faccessat2(unix.AT_FDCWD, "/system/bin/sh", unix.X_OK, unix.AT_EACCESS))
	case "lookpath":
		p, err := exec.LookPath("getprop")
		if err == nil {
			fmt.Print("found ", p)
		}
		return err
	case "statx":
		var st unix.Statx_t
		return r(unix.Statx(unix.AT_FDCWD, ".", 0, unix.STATX_ALL, &st))
	case "openat2":
		fd, err := unix.Openat2(unix.AT_FDCWD, ".", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY})
		if err == nil {
			unix.Close(fd)
		}
		return r(err)
	case "close_range":
		return r(unix.CloseRange(1000, 1100, 0))
	case "flock", "ofd_lock", "fallocate", "copy_file_range", "splice", "sendfile":
		return fileCall(args[0])
	case "pidfd_open":
		_, err := unix.PidfdOpen(os.Getpid(), 0)
		return r(err)
	case "pidfd_send_signal":
		fd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			return err
		}
		return r(unix.PidfdSendSignal(fd, 0, nil, 0))
	case "waitid_pidfd":
		fd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			return err
		}
		var info unix.Siginfo
		err = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED|unix.WNOHANG, nil)
		// A process can't wait for itself: ECHILD means the call ran.
		if errors.Is(err, unix.ECHILD) {
			err = nil
		}
		return r(err)
	case "clone3":
		// A zero size is invalid, so the call fails with EINVAL if it
		// runs at all.
		err := errnoErr(unix.Syscall(unix.SYS_CLONE3, 0, 0, 0))
		if errors.Is(err, unix.EINVAL) {
			err = nil
		}
		return r(err)
	case "pdeathsig":
		return r(unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0))
	case "setsid":
		_, err := unix.Setsid()
		return r(err)
	case "epoll_pwait2":
		err := errnoErr(unix.Syscall6(unix.SYS_EPOLL_PWAIT2, ^uintptr(0), 0, 1, 0, 0, 0))
		if errors.Is(err, unix.EBADF) {
			err = nil
		}
		return r(err)
	case "inotify":
		_, err := unix.InotifyInit1(unix.IN_CLOEXEC)
		return r(err)
	case "getrandom":
		_, err := unix.Getrandom(make([]byte, 16), 0)
		return r(err)
	case "membarrier":
		return r(errnoErr(unix.Syscall(unix.SYS_MEMBARRIER, 0, 0, 0)))
	case "rseq":
		// Bionic registers rseq itself on newer Android, so a second
		// registration fails; either way the call ran.
		err := errnoErr(unix.Syscall6(unix.SYS_RSEQ, 0, 0, 0, 0, 0, 0))
		if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EBUSY) {
			err = nil
		}
		return r(err)
	case "landlock":
		const version = 1
		v, _, e := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, version)
		if e != 0 {
			return e
		}
		fmt.Printf("ABI %d", v)
		return nil
	case "io_uring_setup":
		err := errnoErr(unix.Syscall(unix.SYS_IO_URING_SETUP, 1, 0, 0))
		if errors.Is(err, unix.EFAULT) {
			err = nil
		}
		return r(err)
	case "netlink_getlink":
		return netlinkGetlink()
	}
	return fmt.Errorf("no system call %q", args[0])
}

func errnoErr(_, _ uintptr, e unix.Errno) error {
	if e != 0 {
		return e
	}
	return nil
}

func startAndOutput(cmd *exec.Cmd) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	cmd.Stdout = w
	if err := host.StartLowPriority(cmd); err != nil {
		w.Close()
		r.Close()
		return "", err
	}
	w.Close()
	buf := make([]byte, 256)
	n, _ := r.Read(buf)
	r.Close()
	if err := cmd.Wait(); err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

func fileCall(name string) error {
	a, err := os.CreateTemp(".", "syscall-*")
	if err != nil {
		return err
	}
	defer os.Remove(a.Name())
	defer a.Close()
	if _, err := a.WriteString("0123456789"); err != nil {
		return err
	}
	b, err := os.CreateTemp(".", "syscall-*")
	if err != nil {
		return err
	}
	defer os.Remove(b.Name())
	defer b.Close()
	var callErr error
	switch name {
	case "flock":
		callErr = unix.Flock(int(a.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	case "ofd_lock":
		callErr = unix.FcntlFlock(a.Fd(), unix.F_OFD_SETLK, &unix.Flock_t{Type: unix.F_WRLCK})
	case "fallocate":
		callErr = unix.Fallocate(int(a.Fd()), 0, 0, 4096)
	case "copy_file_range":
		off := int64(0)
		_, callErr = unix.CopyFileRange(int(a.Fd()), &off, int(b.Fd()), nil, 10, 0)
	case "splice":
		var p [2]int
		if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
			return err
		}
		off := int64(0)
		_, callErr = unix.Splice(int(a.Fd()), &off, p[1], nil, 10, 0)
	case "sendfile":
		off := int64(0)
		_, callErr = unix.Sendfile(int(b.Fd()), int(a.Fd()), &off, 10)
	}
	if callErr == nil {
		fmt.Println("allowed")
	}
	return callErr
}

// netlinkGetlink asks the kernel for the network interfaces as Go's
// net.Interfaces does, which Android refuses apps from Android 11 on: the
// reason Pion lists them through wlynxg/anet.
func netlinkGetlink() error {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("bind: %w", err)
	}
	req := struct {
		hdr unix.NlMsghdr
		msg unix.RtGenmsg
	}{
		hdr: unix.NlMsghdr{Len: uint32(unix.SizeofNlMsghdr + unix.SizeofRtGenmsg), Type: unix.RTM_GETLINK, Flags: unix.NLM_F_DUMP | unix.NLM_F_REQUEST, Seq: 1},
		msg: unix.RtGenmsg{Family: unix.AF_UNSPEC},
	}
	b := (*[unix.SizeofNlMsghdr + unix.SizeofRtGenmsg]byte)(unsafe.Pointer(&req))[:]
	if err := unix.Sendto(fd, b, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("sendto: %w", err)
	}
	buf := make([]byte, 1<<16)
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil {
		return fmt.Errorf("recvfrom: %w", err)
	}
	msgs, err := syscall.ParseNetlinkMessage(buf[:n])
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if m.Header.Type == unix.NLMSG_ERROR && len(m.Data) >= 4 {
			code := int32(binary.NativeEndian.Uint32(m.Data[:4]))
			return fmt.Errorf("the kernel answered %v", unix.Errno(-code))
		}
	}
	fmt.Printf("allowed, %d messages", len(msgs))
	return nil
}
