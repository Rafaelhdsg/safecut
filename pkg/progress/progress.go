package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Tracker displays live progress in the terminal: spinner, stage messages,
// and counters. Safe for concurrent use. Falls back to simple line output
// when stdout is not a TTY (piped, CI, etc.).
type Tracker struct {
	w       io.Writer
	mu      sync.Mutex
	stage   string
	detail  string
	frameN  int
	stop    chan struct{}
	stopped chan struct{}
	isTTY   bool
	silent  bool
	start   time.Time
}

// SetSilent suppresses all output (for machine-readable modes like JSON).
func (t *Tracker) SetSilent(s bool) {
	t.silent = s
}

func New() *Tracker {
	return &Tracker{
		w:     os.Stderr,
		isTTY: term.IsTerminal(int(os.Stderr.Fd())),
		start: time.Now(),
	}
}

// Start begins the spinner animation in a background goroutine.
func (t *Tracker) Start(initialStage string) {
	t.stop = make(chan struct{})
	t.stopped = make(chan struct{})
	t.stage = initialStage

	if t.silent {
		close(t.stopped)
		return
	}

	if !t.isTTY {
		fmt.Fprintf(t.w, "  %s\n", initialStage)
		close(t.stopped)
		return
	}

	t.render()
	go t.spin()
}

// Stage updates the current stage label (e.g. "Analyzing idle scores...").
func (t *Tracker) Stage(msg string) {
	if t.silent {
		return
	}
	t.mu.Lock()
	changed := t.stage != msg
	t.stage = msg
	t.detail = ""
	t.mu.Unlock()

	if !t.isTTY && changed {
		fmt.Fprintf(t.w, "  %s\n", msg)
	}
}

// Detail updates the secondary detail line without changing the stage
// (e.g. "  Fetching metrics [3/13]").
func (t *Tracker) Detail(msg string) {
	if t.silent {
		return
	}
	t.mu.Lock()
	t.detail = msg
	t.mu.Unlock()
}

// Finish stops the spinner and prints a final completion message.
func (t *Tracker) Finish(msg string) {
	if t.stop == nil {
		return
	}

	close(t.stop)
	<-t.stopped

	if t.silent {
		return
	}

	if t.isTTY {
		t.clearLines(2)
		elapsed := time.Since(t.start).Truncate(100 * time.Millisecond)
		fmt.Fprintf(t.w, "  \033[32m✓\033[0m %s \033[2m(%s)\033[0m\n", msg, elapsed)
	} else {
		elapsed := time.Since(t.start).Truncate(100 * time.Millisecond)
		fmt.Fprintf(t.w, "  ✓ %s (%s)\n", msg, elapsed)
	}
}

func (t *Tracker) spin() {
	defer close(t.stopped)
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.mu.Lock()
			t.frameN = (t.frameN + 1) % len(frames)
			t.mu.Unlock()
			t.render()
		}
	}
}

func (t *Tracker) render() {
	t.mu.Lock()
	frame := frames[t.frameN]
	stage := t.stage
	detail := t.detail
	elapsed := time.Since(t.start).Truncate(time.Second)
	t.mu.Unlock()

	t.clearLines(2)

	fmt.Fprintf(t.w, "  \033[36m%s\033[0m %s \033[2m%s\033[0m\n", frame, stage, elapsed)
	if detail != "" {
		fmt.Fprintf(t.w, "    \033[2m%s\033[0m\n", detail)
	} else {
		fmt.Fprintln(t.w)
	}
}

func (t *Tracker) clearLines(n int) {
	for i := 0; i < n; i++ {
		fmt.Fprintf(t.w, "\033[2K") // clear line
		if i < n-1 {
			fmt.Fprintf(t.w, "\033[A") // move up
		}
	}
	fmt.Fprintf(t.w, "\r") // move to column 0
}
