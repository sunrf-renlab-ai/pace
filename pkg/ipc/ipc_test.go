package ipc

import (
	"os"
	"testing"
)

func TestServerClientRoundTrip(t *testing.T) {
	// macOS unix socket paths are limited to ~104 chars, so we cannot use
	// t.TempDir() directly (it produces very long paths). Use a short /tmp path.
	sock := shortSocketPath(t)
	t.Setenv("MENTOR_SOCKET_PATH", sock)

	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	srv.Handle("ping", func(req Request) Response {
		return Response{OK: true, Result: "pong"}
	})
	go srv.Serve()

	c, err := Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	resp, err := c.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.OK || resp.Result != "pong" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestUnknownMethod(t *testing.T) {
	sock := shortSocketPath(t)
	t.Setenv("MENTOR_SOCKET_PATH", sock)
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	go srv.Serve()
	c, err := Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	resp, _ := c.Call("nope", nil)
	if resp.OK {
		t.Errorf("expected !OK")
	}
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mns")
	if err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir + "/s"
}
