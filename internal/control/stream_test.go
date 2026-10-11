package control

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

func TestStreamCarriesLinesBothWays(t *testing.T) {
	me, _ := host.CurrentUser()
	got := make(chan Message, 4)
	client, stop := startServer(t, me, map[string]Handler{
		OpShortcuts: func(context.Context, Request) (Reply, error) {
			return Reply{Stream: func(ctx context.Context, s *Stream) error {
				if err := s.Send("hello", map[string]string{"from": "service"}); err != nil {
					return err
				}
				for {
					msg, err := s.Recv()
					if err != nil {
						return err
					}
					got <- msg
					if msg.T == "bye" {
						return s.Send("bye", nil)
					}
				}
			}}, nil
		},
	})
	defer stop()

	s, err := client.Open(Request{Op: OpShortcuts})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	msg, err := s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	var hello map[string]string
	if msg.T != "hello" || json.Unmarshal(msg.D, &hello) != nil || hello["from"] != "service" {
		t.Fatalf("first line = %+v", msg)
	}
	if err := s.Send("press", map[string]bool{"down": true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send("bye", nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"press", "bye"} {
		select {
		case m := <-got:
			if m.T != want {
				t.Fatalf("the service read %q, want %q", m.T, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("the service never read %q", want)
		}
	}
	if msg, err := s.Recv(); err != nil || msg.T != "bye" || msg.D != nil {
		t.Fatalf("last line = %+v, %v", msg, err)
	}
	if _, err := s.Recv(); err == nil {
		t.Fatal("the stream went on after the service's handler returned")
	}
}

func TestStreamIsRefusedLikeAnyRequest(t *testing.T) {
	handlers := map[string]Handler{
		OpShortcuts: func(context.Context, Request) (Reply, error) {
			return Reply{Stream: func(context.Context, *Stream) error { return nil }}, nil
		},
	}
	client, stop := startServer(t, host.Identity{ID: "an-account-that-isnt-this-one", Name: "bob"}, handlers)
	defer stop()
	var userErr *UserError
	if _, err := client.Open(Request{Op: OpShortcuts}); !errors.As(err, &userErr) {
		t.Fatalf("Open as another account = %v, want the service's refusal", err)
	}

	me, _ := host.CurrentUser()
	client, stop = startServer(t, me, map[string]Handler{})
	defer stop()
	if _, err := client.Open(Request{Op: OpShortcuts}); !errors.As(err, &userErr) {
		t.Fatalf("Open of an operation the service lacks = %v, want its refusal", err)
	}
}

func TestStreamPingsKeepItOpen(t *testing.T) {
	ping, idle := StreamPing, StreamIdle
	StreamPing, StreamIdle = 20*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { StreamPing, StreamIdle = ping, idle })

	me, _ := host.CurrentUser()
	ended := make(chan error, 1)
	client, stop := startServer(t, me, map[string]Handler{
		OpShortcuts: func(context.Context, Request) (Reply, error) {
			return Reply{Stream: func(ctx context.Context, s *Stream) error {
				for {
					if _, err := s.Recv(); err != nil {
						ended <- err
						return err
					}
				}
			}}, nil
		},
	})
	defer stop()
	s, err := client.Open(Request{Op: OpShortcuts})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, quiet := context.WithCancel(context.Background())
	go s.KeepAlive(ctx)
	select {
	case err := <-ended:
		t.Fatalf("the service gave up on a stream that pinged: %v", err)
	case <-time.After(5 * StreamIdle / 2):
	}
	quiet()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the service kept a stream that went quiet")
	}
}
