// Package script runs the sysop's JavaScript (ECMAScript 5.1 and much of
// 6, by goja) as part of the BBS: a script draws the shared screens of
// internal/view, so it works on every terminal the BBS has (text, RIP,
// 3270), and acts on the BBS as its caller, with their access.
//
// A script has no files, network or processes: only what the API here
// gives.  It may compute for Limit between one screen and the next before
// it is stopped.
package script

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// DefaultLimit is how long a script may compute between screens.
const DefaultLimit = 2 * time.Second

// maxSource is the largest script read.
const maxSource = 1 << 20

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidName: a script's name is 1 to 32 lower case letters, digits, _ and
// -, starting with a letter or digit; it is NAME.js in the directory.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// ErrNoScript: there is no script of that name.
var ErrNoScript = errors.New("there is no such script")

// Engine runs the scripts of a directory, compiling each once (and again
// when it changes).
type Engine struct {
	Dir   string
	Limit time.Duration

	mu    sync.Mutex
	cache map[string]compiled
}

type compiled struct {
	mod  time.Time
	size int64
	prog *goja.Program
}

var engines sync.Map // dir: *Engine

// For is the engine of a directory, shared by every session.
func For(dir string) *Engine {
	e, _ := engines.LoadOrStore(dir, &Engine{Dir: dir, Limit: DefaultLimit, cache: map[string]compiled{}})
	return e.(*Engine)
}

// path is the script name's file: NAME.js in the directory.
func (e *Engine) path(name string) string { return filepath.Join(e.Dir, name+".js") }

// Has: the directory has the script.
func (e *Engine) Has(name string) bool {
	if !ValidName(name) {
		return false
	}
	fi, err := os.Stat(e.path(name))
	return err == nil && fi.Mode().IsRegular()
}

// Names are the scripts in the directory.
func (e *Engine) Names() []string {
	ms, _ := filepath.Glob(filepath.Join(e.Dir, "*.js"))
	var out []string
	for _, m := range ms {
		if n := strings.TrimSuffix(filepath.Base(m), ".js"); ValidName(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// program is the script compiled.
func (e *Engine) program(name string) (*goja.Program, error) {
	if !ValidName(name) {
		return nil, ErrNoScript
	}
	p := e.path(name)
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, ErrNoScript
	}
	e.mu.Lock()
	c, ok := e.cache[name]
	e.mu.Unlock()
	if ok && c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
		return c.prog, nil
	}
	if fi.Size() > maxSource {
		return nil, fmt.Errorf("%s.js is larger than %d bytes", name, maxSource)
	}
	src, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	prog, err := goja.Compile(name+".js", string(src), true)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.cache[name] = compiled{mod: fi.ModTime(), size: fi.Size(), prog: prog}
	e.mu.Unlock()
	return prog, nil
}

// Check compiles a script, saying what is wrong with it.
func (e *Engine) Check(name string) error {
	_, err := e.program(name)
	return err
}
