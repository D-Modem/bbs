package script

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// UI is what a script draws on: the caller's front end.
type UI interface {
	view.UI
	Kind() string
	ShowScreen(name string, wait bool) (bool, error)
}

// How a run ends, other than by its end.
var (
	errExit    = errors.New("exit")
	errTooLong = errors.New("the script computed too long")
)

// run is a script running for a caller.
type run struct {
	name string
	b    *bbs.BBS
	n    *bbs.Node
	ui   UI
	vm   *goja.Runtime

	busy  atomic.Int64 // when the script began computing (0: it waits on a screen)
	fatal error        // the caller went (the front end's error): the run ends with it
}

// Run runs a script for the caller on node n.  It returns the front end's
// errors (the caller hung up, idled): a script's own failure is logged and
// told to the caller, and is not an error of the session.
func (e *Engine) Run(name string, b *bbs.BBS, n *bbs.Node, ui UI) error {
	prog, err := e.program(name)
	if err != nil {
		b.Log.Printf("node %d: script %s: %v", n.Num, name, err)
		ui.Say(view.Bad, "The script "+name+" would not run.")
		return nil
	}
	r := &run{name: name, b: b, n: n, ui: ui, vm: goja.New()}
	r.vm.SetMaxCallStackSize(1000)
	r.vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	if err := r.bind(); err != nil {
		return err
	}
	limit := e.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	done := make(chan struct{})
	go r.watch(limit, done)
	r.busy.Store(time.Now().UnixNano())
	_, err = r.vm.RunProgram(prog)
	close(done)
	return r.end(err)
}

// watch stops the script when it has computed for limit without a screen.
func (r *run) watch(limit time.Duration, done chan struct{}) {
	t := time.NewTicker(limit / 20)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-t.C:
			if since := r.busy.Load(); since != 0 && now.UnixNano()-since > int64(limit) {
				r.vm.Interrupt(errTooLong)
				return
			}
		}
	}
}

// wait is a screen: the time the caller takes is not the script's.
func (r *run) wait(f func() error) {
	r.busy.Store(0)
	err := f()
	r.busy.Store(time.Now().UnixNano())
	if err != nil {
		// the caller went: the script ends, whatever it catches
		r.fatal = err
		r.vm.Interrupt(err)
		panic(r.vm.NewGoError(err))
	}
}

// end is how the run ended, for the session.
func (r *run) end(err error) error {
	if r.fatal != nil {
		return r.fatal
	}
	if err == nil {
		return nil
	}
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		switch ie.Value() {
		case errExit:
			return nil
		case errTooLong:
			r.b.Log.Printf("node %d: script %s: stopped: computed longer than allowed between screens", r.n.Num, r.name)
			r.ui.Say(view.Bad, "The script "+r.name+" took too long and was stopped.")
			return nil
		}
	}
	msg := err.Error()
	var ex *goja.Exception
	if errors.As(err, &ex) {
		msg = ex.String()
	}
	msg = strings.TrimSpace(msg)
	r.b.Log.Printf("node %d: script %s: %s", r.n.Num, r.name, msg)
	if u := r.n.User(); u != nil && u.Sysop {
		r.ui.Say(view.Bad, "The script "+r.name+" failed: "+firstLine(msg))
	} else {
		r.ui.Say(view.Bad, "The script "+r.name+" failed.")
	}
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// throw is a JavaScript error, for a script's mistake (a bad argument, a
// refusal): it may catch it.
func (r *run) throw(format string, a ...any) {
	panic(r.vm.NewGoError(fmt.Errorf(format, a...)))
}
