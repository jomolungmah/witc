// Package progress renders lightweight, interactive progress feedback to a
// writer (typically stderr). Output goes to stderr so it never corrupts the
// summary written to stdout or an output file, and rendering is a no-op when
// disabled, so callers don't need to special-case quiet or non-terminal runs.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Reporter renders progress lines to a writer using carriage returns so each
// update overwrites the previous one in place. A nil or disabled Reporter
// turns every method into a no-op.
type Reporter struct {
	w       io.Writer
	enabled bool

	mu    sync.Mutex
	width int // width of the last rendered line, used to clear leftovers
}

// New returns a Reporter writing to w. Progress is only rendered when enabled;
// pass IsTerminal(os.Stderr) so piped or redirected runs stay clean.
func New(w io.Writer, enabled bool) *Reporter {
	return &Reporter{w: w, enabled: enabled}
}

// IsTerminal reports whether f refers to a character device (an interactive
// terminal), meaning animated progress is appropriate.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

const barWidth = 24

// Step renders a determinate progress bar, e.g. "Parsing files [████░░] 12/40".
// total <= 0 renders an empty bar.
func (r *Reporter) Step(label string, current, total int) {
	if r == nil || !r.enabled {
		return
	}
	filled := 0
	if total > 0 {
		filled = current * barWidth / total
		if filled > barWidth {
			filled = barWidth
		}
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	r.render(fmt.Sprintf("%s [%s] %d/%d", label, bar, current, total))
}

// Spin starts an indeterminate spinner for a phase whose progress can't be
// measured (e.g. type-checking the dependency tree) and returns a stop
// function. Calling stop ends the animation and clears the line; it is safe to
// call once and is a no-op when the Reporter is disabled.
func (r *Reporter) Spin(label string) (stop func()) {
	if r == nil || !r.enabled {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		r.render(fmt.Sprintf("%c %s", frames[0], label))
		for i := 1; ; i++ {
			select {
			case <-done:
				return
			case <-ticker.C:
				r.render(fmt.Sprintf("%c %s", frames[i%len(frames)], label))
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-stopped // wait for the goroutine to stop rendering before clearing
			r.clear()
		})
	}
}

// Done overwrites the current line with msg and terminates it with a newline,
// leaving a final status visible.
func (r *Reporter) Done(msg string) {
	if r == nil || !r.enabled {
		return
	}
	r.render(msg)
	r.mu.Lock()
	fmt.Fprint(r.w, "\n")
	r.width = 0
	r.mu.Unlock()
}

// render writes line in place, padding with spaces to erase any leftover
// characters from a previously longer line.
func (r *Reporter) render(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pad := ""
	if l := displayWidth(line); l < r.width {
		pad = strings.Repeat(" ", r.width-l)
	}
	fmt.Fprintf(r.w, "\r%s%s", line, pad)
	r.width = displayWidth(line)
}

// clear erases the current line and returns the cursor to its start.
func (r *Reporter) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.width > 0 {
		fmt.Fprintf(r.w, "\r%s\r", strings.Repeat(" ", r.width))
		r.width = 0
	}
}

// displayWidth approximates the terminal cell width of s by counting runes
// rather than bytes, so multibyte bar/spinner glyphs don't inflate padding.
func displayWidth(s string) int {
	return len([]rune(s))
}
