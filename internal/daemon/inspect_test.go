package daemon

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/instance"
)

func TestInspect(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		p := testPaths(t)
		t.Setenv("MASIL_SERVER", "")
		cfg := config.Defaults() // Server empty/default -> AutostartServer = http://localhost:8787
		r := Inspect(p, cfg)
		if r.Running || r.LoggedIn || r.State != nil || !r.DesktopEnabled || r.ConfigServer != "http://localhost:8787" || r.ServerSource != "config" {
			t.Fatalf("empty: %+v", r)
		}

		saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A")
		saveState(p, State{UserID: "01JA0000000000000000000SAM", Epoch: "E1", LastSeq: 1041, SavedAt: t0})
		log, closer, _ := NewLogger(p, nil)
		log.Warn("credentials server mismatch", "credentials_server", "http://127.0.0.1:9999")
		log.Info("realtime ready")
		closer.Close()
		release, _ := instance.AcquireDaemon(p)
		defer release()

		r = Inspect(p, cfg)
		if !r.Running || !r.LoggedIn || r.Handle != "sam" || !r.ServerMismatch || r.State.LastSeq != 1041 {
			t.Fatalf("report %+v", r)
		}
		if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "credentials server mismatch") {
			t.Fatalf("problems %q", r.Problems)
		}
	})

	// Review Focus 1: logged in with MASIL_SERVER=local, autostarted daemon can't see it,
	// and `masil daemon status` is run from that same shell.
	t.Run("shell_env_not_seen_by_daemon", func(t *testing.T) {
		p := testPaths(t)
		t.Setenv("MASIL_SERVER", "http://127.0.0.1:9999")
		cfg := config.Defaults()
		saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A")
		r := Inspect(p, cfg) // daemon not running -> predict the autostart view
		if r.ConfigServer != "http://localhost:8787" || r.ServerSource != "config" || !r.ServerMismatch || r.ShellServer != "http://127.0.0.1:9999" {
			t.Fatalf("shell env leaked into daemon view: %+v", r)
		}

		// A running daemon that recorded what it actually sees wins over the prediction.
		release, _ := instance.AcquireDaemon(p)
		defer release()
		writeRunStatus(p, runStatus{ConfigServer: "http://localhost:8787", CredentialsServer: "http://127.0.0.1:9999", ServerMismatch: true, UpdatedAt: t0})
		if r := Inspect(p, cfg); r.ServerSource != "daemon" || !r.ServerMismatch {
			t.Fatalf("running daemon view: %+v", r)
		}
		writeRunStatus(p, runStatus{ConfigServer: "http://127.0.0.1:9999", CredentialsServer: "http://127.0.0.1:9999", EnvServer: true, UpdatedAt: t0}) // started by hand in that shell
		if r := Inspect(p, cfg); r.ServerSource != "daemon" || r.ServerMismatch || r.ShellServer != "" {
			t.Fatalf("hand-started daemon: %+v", r)
		}
	})

	t.Run("autostart_server", func(t *testing.T) {
		t.Setenv("MASIL_SERVER", "http://ignored.example")
		c := config.Defaults()
		c.Server = "https://masil.example.workers.dev/"
		if got := AutostartServer(c); got != "https://masil.example.workers.dev" {
			t.Fatalf("AutostartServer = %q", got)
		}
	})
}

func TestInspectProblemsOnlyWarnAndErrorLevels(t *testing.T) {
	p := testPaths(t)
	log, closer, err := NewLogger(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("not logged in; waiting for credentials")
	log.Warn("w1")
	log.Error("unexpected realtime error")
	log.Warn("w2")
	log.Info("mentions level=WARN in the message") // an INFO line, despite the text
	log.Warn("w3")
	closer.Close()

	got := Inspect(p, config.Defaults()).Problems
	if len(got) != 3 || !strings.Contains(got[0], "unexpected realtime error") || !strings.Contains(got[2], "msg=w3") {
		t.Fatalf("problems %q", got)
	}
	for _, l := range got {
		if strings.Contains(l, "level=INFO") {
			t.Fatalf("INFO line in problems: %q", l)
		}
	}
}
