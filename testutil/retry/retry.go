// Package retry provides support for repeating operations in tests.
//
// A sample retry operation looks like this:
//
//   func TestX(t *testing.T) {
//       for r := retry.OneSec(); r.NextOr(t.FailNow); {
//           if err := foo(); err != nil {
//               t.Log("f: ", err)
//               continue
//           }
//           break
//       }
//   }
//
// A sample retry operation which exits a test with a message
// looks like this:
//
//   func TestX(t *testing.T) {
//       for r := retry.OneSec(); r.NextOr(func(){ t.Fatal("foo failed") }); {
//           if err := foo(); err != nil {
//               t.Log("f: ", err)
//               continue
//           }
//           break
//       }
//   }
package retry

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type R struct {
	fail   bool
	output []string
}

func (r *R) FailNow() {
	r.fail = true
	runtime.Goexit()
}

func (r *R) Fatal(args ...interface{}) {
	r.log(fmt.Sprint(args...))
	r.FailNow()
}

func (r *R) Fatalf(format string, args ...interface{}) {
	r.log(fmt.Sprintf(format, args))
	r.FailNow()
}

func (r *R) Error(args ...interface{}) {
	r.log(fmt.Sprint(args...))
	r.fail = true
}

func (r *R) log(s string) {
	r.output = append(r.output, decorate(s))
}

func decorate(s string) string {
	_, file, line, ok := runtime.Caller(3)
	if ok {
		n := strings.LastIndex(file, "/")
		if n >= 0 {
			file = file[n+1:]
		}
	} else {
		file = "???"
		line = 1
	}
	return fmt.Sprintf("%s:%d: %s", file, line, s)
}

func Run(t *testing.T, f func(r *R)) {
	run(OneSec(), t, f)
}

func RunWith(r Retryer, t *testing.T, f func(r *R)) {
	run(r, t, f)
}

func dedup(a []string) string {
	if len(a) == 0 {
		return ""
	}
	m := map[string]int{}
	for _, s := range a {
		m[s] = m[s] + 1
	}
	var b bytes.Buffer
	for _, s := range a {
		if _, ok := m[s]; ok {
			b.WriteString(s)
			b.WriteRune('\n')
			delete(m, s)
		}
	}
	return string(b.Bytes())
}

func run(r Retryer, t *testing.T, f func(r *R)) {
	rr := &R{}
	fail := func() {
		out := dedup(rr.output)
		if out != "" {
			t.Log(out)
		}
		t.FailNow()
	}
	for r.NextOr(fail) {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			f(rr)
		}()
		wg.Wait()
		if rr.fail {
			rr.fail = false
			continue
		}
		break
	}
}

// OneSec repeats an operation for one second and waits 25ms in between.
func OneSec() *Timer {
	return &Timer{Timeout: time.Second, Wait: 25 * time.Millisecond}
}

// ThreeTimes repeats an operation three times and waits 25ms in between.
func ThreeTimes() *Counter {
	return &Counter{Count: 3, Wait: 25 * time.Millisecond}
}

// Retryer provides an interface for repeating operations
// until they succeed or an exit condition is met.
type Retryer interface {
	// NextOr returns true if the operation should be repeated.
	// Otherwise, it calls fail and returns false.
	NextOr(fail func()) bool
}

// Counter repeats an operation a given number of
// times and waits between subsequent operations.
type Counter struct {
	Count int
	Wait  time.Duration

	count int
}

func (r *Counter) NextOr(fail func()) bool {
	if r.count == r.Count {
		fail()
		return false
	}
	if r.count > 0 {
		time.Sleep(r.Wait)
	}
	r.count++
	return true
}

// Timer repeats an operation for a given amount
// of time and waits between subsequent operations.
type Timer struct {
	Timeout time.Duration
	Wait    time.Duration

	// stop is the timeout deadline.
	// Set on the first invocation of Next().
	stop time.Time
}

func (r *Timer) NextOr(fail func()) bool {
	if r.stop.IsZero() {
		r.stop = time.Now().Add(r.Timeout)
		return true
	}
	if time.Now().After(r.stop) {
		fail()
		return false
	}
	time.Sleep(r.Wait)
	return true
}
