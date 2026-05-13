package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"os/exec"

	"github.com/sunrf-renlab-ai/pace/pkg/action"
	"github.com/sunrf-renlab-ai/pace/pkg/brain"
	"github.com/sunrf-renlab-ai/pace/pkg/ingest"
	"github.com/sunrf-renlab-ai/pace/pkg/ipc"
	"github.com/sunrf-renlab-ai/pace/pkg/loop"
	"github.com/sunrf-renlab-ai/pace/pkg/notify"
	"github.com/sunrf-renlab-ai/pace/pkg/oauth"
	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

type Daemon struct {
	State    *state.State
	server   *http.Server
	listener net.Listener
	loop     *loop.Loop
	cancel   context.CancelFunc
	brain    loop.Decider
	actions  *action.Registry
	ipc      *ipc.Server
}

func Start() (*Daemon, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg := filepath.Join(home, ".config", "pace")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return nil, err
	}
	st, err := state.Open(filepath.Join(cfg, "state.db"))
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		st.Close()
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	portFile := filepath.Join(cfg, "port")
	tmp := portFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d\n", port)), 0o644); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}
	if err := os.Rename(tmp, portFile); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/event", ingest.NewHandler(st))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	srv := &http.Server{Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go srv.Serve(ln)

	n := notify.New()
	reg := action.NewRegistry(n)

	// Brain wiring: try to construct one if `claude` is on PATH.
	// v0.5: there are no rules. Without brain, the loop becomes a no-op.
	var brn loop.Decider
	if claudePath, err := exec.LookPath("claude"); err == nil {
		authEnv, _ := oauth.LoadAuthEnv() // nil if no token; subprocess inherits user's claude auth
		brn = brain.New(claudePath, authEnv)
		reg.Register("spawn_session", &action.SpawnExec{ClaudePath: claudePath, AuthEnv: authEnv})
	}

	ctx, cancel := context.WithCancel(context.Background())
	l := loop.New(st, brn, reg)
	l.Start(ctx)

	d := &Daemon{State: st, server: srv, listener: ln, loop: l, cancel: cancel, brain: brn, actions: reg}

	if sock, err := ipc.NewServer(); err == nil {
		r := &rpc{d: d}
		sock.Handle("status", r.status)
		sock.Handle("pause", r.pause)
		sock.Handle("undo", r.undo)
		sock.Handle("actions", r.actions)
		sock.Handle("chat", r.chat)
		// v0.3 PM handlers
		sock.Handle("goal.set", r.goalSet)
		sock.Handle("goal.get", r.goalGet)
		sock.Handle("goal.delete", r.goalDelete)
		sock.Handle("focus.set", r.focusSet)
		sock.Handle("focus.get", r.focusGet)
		sock.Handle("focus.clear", r.focusClear)
		sock.Handle("plan.show", r.planShow)
		sock.Handle("plan.generate", r.planGenerate)
		// v0.4 mentor handlers
		sock.Handle("mentor.list", r.mentorList)
		sock.Handle("mentor.ack", r.mentorAck)
		sock.Handle("mentor.dismiss", r.mentorDismiss)
		sock.Handle("mentor.ask", r.mentorAsk)
		sock.Handle("mentor.review", r.mentorReview)
		sock.Handle("mentor.consult", r.mentorConsult)
		go sock.Serve()
		d.ipc = sock
	}

	return d, nil
}

func (d *Daemon) Stop() error {
	if d.ipc != nil {
		d.ipc.Close()
	}
	if d.cancel != nil {
		d.cancel()
	}
	if d.loop != nil {
		d.loop.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.server.Shutdown(ctx)
	d.State.Close()
	home, _ := os.UserHomeDir()
	os.Remove(filepath.Join(home, ".config", "pace", "port"))
	return nil
}
