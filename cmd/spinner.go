package cmd

import (
	"fmt"
	"sync"
	"time"

	"github.com/keeandrews/loradex-cli/internal/output"
)

// spinner renders an animated, elapsed-time status line for a long, silent
// operation (e.g. loading a multi-GB model) so it never looks frozen. It is a
// no-op when progress output is disabled (non-TTY / --json), and stop() is safe
// to call more than once.
type spinner struct {
	stopCh chan struct{}
	doneCh chan struct{}
	once   sync.Once
}

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// startSpinner begins animating msg on p.Err and returns a handle to stop it.
func startSpinner(p *output.Printer, msg string) *spinner {
	s := &spinner{stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	if !p.ProgressEnabled() {
		close(s.doneCh)
		return s
	}
	go func() {
		defer close(s.doneCh)
		start := time.Now()
		t := time.NewTicker(120 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-s.stopCh:
				fmt.Fprint(p.Err, "\r\033[K") // clear the spinner line
				return
			case <-t.C:
				elapsed := int(time.Since(start).Seconds())
				fmt.Fprintf(p.Err, "\r  %c %s (%ds)", spinnerFrames[i%len(spinnerFrames)], msg, elapsed)
				i++
			}
		}
	}()
	return s
}

// stop halts the spinner and clears its line. Safe to call multiple times.
func (s *spinner) stop() {
	s.once.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}
