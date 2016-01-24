// Copyright 2014, Surul Software Labs GmbH
// All rights reserved.

/*
Package testfault provides utilities to help with testing in conjunction with the fault library.
*/
package testfault

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/surullabs/fault"
)

type site struct {
	fn   string
	call *fault.Call
	err  error
}

type Recording []site

type recorder struct {
	sites []site
}

func (r Recording) ErrorMatches(index int, match string) bool {
	if index >= len(r) {
		return false
	}
	if r[index].err == nil {
		return false
	}
	return regexp.MustCompile(match).MatchString(r[index].err.Error())
}

func (r Recording) TrackErrors(e Recording) (err error) {
	if len(e) > len(r) {
		return fmt.Errorf("error recording has %d entries while master has %d", len(e), len(r))
	}
	for i, s := range e {
		if !r[i].call.Equal(s.call) || r[i].fn != s.fn {
			return fmt.Errorf("recording number %d doesn't match master", i)
		}
		if s.err != nil {
			r[i].err = s.err
		}
	}
	return
}

func (r Recording) AllErrorsSeen() bool {
	for _, s := range r {
		if s.err == nil {
			return false
		}
	}
	return true
}

func newRecorder() *recorder { return &recorder{sites: make([]site, 0)} }

func (r *recorder) last() int { return len(r.sites) - 1 }

func (r *recorder) record() {
	stack := fault.ReadStack("")
	callIndex := -1
	for i, call := range stack {
		if strings.HasPrefix(call.Name, testCheckerPrefix) {
			callIndex = i
			break
		}
	}
	site := site{}
	if callIndex != -1 && len(stack) > (callIndex+1) {
		site.call = &stack[callIndex+1]
		site.fn = strings.TrimPrefix(stack[callIndex].Name, testCheckerPrefix)
	}
	r.sites = append(r.sites, site)
}

func (r *recorder) trackError(err error) {
	if len(r.sites) > 0 {
		r.sites[len(r.sites)-1].err = err
	}
}

var testCheckerPrefix = fault.TypePrefix(&TestChecker{}) + "."

type failure struct {
	index int
	err   error
}

func (f *failure) fail(index int) error {
	if f != nil && f.index == index {
		return f.err
	}
	return nil

}

// TestChecker provides an implementation of Checker for use in tests.
type TestChecker struct {
	recorder *recorder
	fail     *failure
	checker  fault.FaultCheck
	onError  func(...interface{})
}

func NewTestChecker(onError func(...interface{})) *TestChecker {
	checker := fault.NewChecker().SetFaulter(&fault.DebugFaulter{fault.TypePrefix(&TestChecker{})})
	return &TestChecker{recorder: newRecorder(), checker: checker, onError: onError}
}

type Resetter struct {
	Reset func()
}

func (t *TestChecker) StartRecording() { t.recorder = newRecorder() }

func (t *TestChecker) Recording() Recording { return t.recorder.sites }

// Fail will create an artificial failure for the i'th call to one of the
// checking methods. Only one failure can be enqueued at a time.
func (t *TestChecker) FailAt(index int, err error) { t.fail = &failure{index, err} }

// ResetFailures clears any enqueued failures.
func (t *TestChecker) ResetFailures() { t.fail = nil }

func (t *TestChecker) failNow() error { return t.fail.fail(t.recorder.last()) }

func (t *TestChecker) Patch(orig *fault.FaultCheck) Resetter {
	original := *orig
	t.checker = original
	*orig = t
	return Resetter{func() { *orig = original }}
}

func (t *TestChecker) OnError(fn func(...interface{})) Resetter {
	orig := t.onError
	t.onError = fn
	return Resetter{func() { t.onError = orig }}
}

func (t *TestChecker) RecoverPanic(errPtr *error, panicked interface{}) {
	t.checker.RecoverPanic(errPtr, panicked)
	t.recorder.trackError(*errPtr)
	if t.onError != nil && *errPtr != nil {
		t.onError("\n" + fault.VerboseTrace(*errPtr))
	}
}

// Recover implements FaultCheck.Recover
func (t *TestChecker) Recover(errPtr *error) {
	t.RecoverPanic(errPtr, recover())
}

// True implements FaultCheck.True
func (t *TestChecker) True(condition bool, errStr string) {
	t.recorder.record()
	if failed := t.failNow(); failed != nil {
		condition = false
	}
	t.checker.True(condition, errStr)
}

// Truef implements FaultCheck.Truef
func (t *TestChecker) Truef(condition bool, format string, args ...interface{}) {
	t.recorder.record()
	if failed := t.failNow(); failed != nil {
		condition = false
	}
	t.checker.Truef(condition, format, args...)
}

// Return implements FaultCheck.Return
func (t *TestChecker) Return(i interface{}, err error) interface{} {
	t.recorder.record()
	if failed := t.failNow(); failed != nil && err == nil {
		err = failed
	}
	return t.checker.Return(i, err)
}

// Error implements FaultCheck.Error
func (t *TestChecker) Error(err error) {
	t.recorder.record()
	if failed := t.failNow(); failed != nil && err == nil {
		err = failed
	}
	t.checker.Error(err)
}

// Output implements FaultCheck.Output
func (t *TestChecker) Output(i interface{}, err error) interface{} {
	t.recorder.record()
	if failed := t.failNow(); failed != nil && err == nil {
		err = failed
	}
	return t.checker.Output(i, err)
}

func (t *TestChecker) Failure(err error) fault.Fault {
	t.recorder.record()
	if failed := t.failNow(); failed != nil && err == nil {
		err = failed
	}
	return t.checker.Failure(err)
}
