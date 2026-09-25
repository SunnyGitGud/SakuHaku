package watchparty

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestRealMPV drives an actual mpv over IPC. Skipped when mpv isn't installed.
func TestRealMPV(t *testing.T) {
	mpv, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("mpv not installed")
	}
	sock := filepath.Join(t.TempDir(), "mpv.sock")
	if runtime.GOOS == "windows" {
		sock = fmt.Sprintf(`\\.\pipe\sakuhaku-test-%d`, time.Now().UnixNano())
	}
	cmd := exec.Command(mpv, "--no-config", "--vo=null", "--ao=null", "--idle=no",
		"--input-ipc-server="+sock, "av://lavfi:testsrc=duration=120:size=320x240:rate=24")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	p, err := DialMPV(sock, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	waitEvent := func(want EventKind) {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case ev := <-p.Events():
				if ev.Kind == want {
					return
				}
			case <-timeout:
				t.Fatalf("no event %v", want)
			}
		}
	}

	// Wait for playback to start
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := p.State()
		if err == nil && st.Pos > 0.2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mpv never started playing: %+v %v", st, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := p.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	waitEvent(EventPause)
	st, _ := p.State()
	if !st.Paused {
		t.Fatal("not paused")
	}

	if err := p.Seek(60); err != nil {
		t.Fatal(err)
	}
	waitEvent(EventSeek)
	st, _ = p.State()
	if st.Pos < 59.9 || st.Pos > 60.1 {
		t.Fatalf("pos after seek = %v", st.Pos)
	}

	if err := p.SetSpeed(1.05); err != nil {
		t.Fatal(err)
	}
	p.SetPaused(false)
	waitEvent(EventPlay)
	st, _ = p.State()
	if st.Paused || st.Speed != 1.05 {
		t.Fatalf("state = %+v", st)
	}
	p.ShowText("hello")
}
