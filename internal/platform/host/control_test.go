package host

import (
	"bufio"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestControlEndpointIdentifiesBothEnds(t *testing.T) {
	me, err := CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	path := testControlPath(t)
	ln, err := ListenControl(path, me)
	if err != nil {
		t.Fatalf("ListenControl: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			serverErr <- err
			return
		}
		peer, err := PeerIdentity(conn)
		if err != nil {
			serverErr <- err
			return
		}
		_, err = conn.Write([]byte(peer.ID + " " + line))
		serverErr <- err
	}()

	conn, err := DialControl(path, ControlServer{Account: me}, 5*time.Second)
	if err != nil {
		t.Fatalf("DialControl: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
	if want := me.ID + " hello\n"; reply != want {
		t.Fatalf("reply = %q, want %q", reply, want)
	}
}

func TestDialControlRejectsUnexpectedServer(t *testing.T) {
	me, err := CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	path := testControlPath(t)
	ln, err := ListenControl(path, me)
	if err != nil {
		t.Fatalf("ListenControl: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	other := Identity{ID: otherAccountID(), Name: "someone else"}
	conn, err := DialControl(path, ControlServer{Account: other}, 5*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("DialControl trusted a server running as the wrong account")
	}
	if !strings.Contains(err.Error(), "not") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestControlReadDeadlineAndListenerClose(t *testing.T) {
	me, err := CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	path := testControlPath(t)
	ln, err := ListenControl(path, me)
	if err != nil {
		t.Fatalf("ListenControl: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
		close(accepted)
	}()
	client, err := DialControl(path, ControlServer{Account: me}, 5*time.Second)
	if err != nil {
		t.Fatalf("DialControl: %v", err)
	}
	defer client.Close()
	server := <-accepted
	if server == nil {
		t.Fatal("accept failed")
	}
	defer server.Close()

	// A client that never writes must not block the server forever.
	if err := server.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = server.Read(make([]byte, 16))
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Read error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("deadline took %v", elapsed)
	}

	// Close must unblock a pending Accept.
	acceptErr := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		acceptErr <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-acceptErr:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Accept after Close = %v, want net.ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after Close")
	}
}
