// Copyright 2014, Surul Software Labs GmbH
// All rights reserved.

package testfault

import (
	"errors"
	"fmt"
	"github.com/surullabs/fault"
	"strings"
	"testing"
)

var _ = fmt.Sprintf

func TestPatching(t *testing.T) {
	var checker fault.FaultCheck = fault.NewChecker().SetFaulter(fault.Simple)
	var origChecker = checker
	tester := NewTestChecker(nil)
	defer func() {
		if origChecker != checker {
			t.Error("Patch not reset")
		}
	}()
	defer tester.Patch(&checker).Reset()
	if checker != tester {
		t.Error("Patch not done")
	}

	var err error
	reader := func(args ...interface{}) { err = args[0].(error) }
	if tester.onError != nil {
		t.Error("on error already set")
	}
	defer func() {
		if tester.onError != nil {
			t.Error("on error not reset")
		}
	}()
	defer tester.OnError(reader).Reset()
	if tester.onError == nil {
		t.Error("on error not patched")
	}

	recovered := func() (recErr error) {
		// NOTE: Use checker here to simulate usage in a regular package.
		defer checker.Recover(&recErr)
		checker.True(false, "expected error")
		return nil
	}()

	if err == nil || recovered == nil || recovered.Error() != err.Error() {
		t.Error("Did not find expected error")
	}
}

func checkReturn(errstr string, pass bool) (int, error) { return 0, checkErr(errstr, pass) }

func checkErr(errstr string, pass bool) error {
	if pass {
		return nil
	}
	return errors.New(errstr)
}

func errorGen(checker fault.FaultCheck, pass ...bool) (err error) {
	defer checker.Recover(&err)
	checker.True(pass[0], "true error")
	checker.Truef(pass[1], "truef %s", "error")
	checker.Return(checkReturn("return error", pass[2]))
	checker.Error(checkErr("error check", pass[3]))
	checker.Output(checkReturn("output error", pass[4]))
	return
}

type errorGenTest struct {
	pass  []bool
	sites []site
}

var errorExpected = []struct {
	fn, err string
}{
	{"True", "true error"},
	{"Truef", "truef error"},
	{"Return", "return error"},
	{"Error", "error check"},
	{"Output", "output error; output: 0"},
}

func genCases() (cases []errorGenTest) {
	cases = make([]errorGenTest, 32)
	for i := 0; i < 1<<5; i++ {
		t := errorGenTest{}
		t.pass = make([]bool, 5)
		t.sites = make([]site, 0)
		var j uint
		firstFalse := -1
		for j = 0; j < 5; j++ {
			t.pass[j] = (i & (1 << j)) != 0
			if firstFalse == -1 {
				t.sites = append(t.sites, mksite(t.pass[j], errorExpected[j].fn, errorExpected[j].err))
			}

			if firstFalse == -1 && !t.pass[j] {
				firstFalse = int(j)
			}
		}
		cases[i] = t
	}
	return
}

func mksite(pass bool, name, err string) site {
	site := site{fn: name}
	if !pass {
		site.err = errors.New(err)
	}
	return site
}

func checkResults(t *testing.T, expected, obtained []site) {
	if len(obtained) != len(expected) {
		t.Error("Mismatched number of sites")
	}
	for i, testSite := range expected {
		recorded := obtained[i]
		if testSite.fn != recorded.fn {
			t.Error("Function mismatch in obtained")
		}
		if testSite.err == nil {
			if recorded.err != nil {
				t.Error("Unexpected error recorded")
			}
		} else {
			if recorded.err == nil {
				t.Error("No error recorded")
			} else if recorded.err.Error() != testSite.err.Error() {
				t.Error("Wrong error recorded")
			}
		}
	}
}

func TestRecording(t *testing.T) {
	var checker fault.FaultCheck = fault.NewChecker().SetFaulter(fault.Simple)
	tester := NewTestChecker(nil)
	defer tester.Patch(&checker).Reset()
	cases := genCases()

	for _, tx := range cases {
		tester.StartRecording()
		errorGen(checker, tx.pass...)
		checkResults(t, tx.sites, tester.Recording())
	}
}

func TestTrackErrors(t *testing.T) {
	var checker fault.FaultCheck = fault.NewChecker().SetFaulter(fault.Simple)
	tester := NewTestChecker(nil)
	defer tester.Patch(&checker).Reset()
	cases := genCases()
	// First run with no errors
	clean := cases[31] // All true
	tester.StartRecording()
	noErr := errorGen(checker, clean.pass...)
	if noErr != nil {
		t.Error("Found an error for clean run")
	}

	template := tester.Recording() // This is the clean master
	// Now run everything except the run with true, true, true, true, false (#15 -> 01111) .. it's all reversed because of how the cases are generated.
	for i, tx := range cases {
		if i == 15 {
			continue
		}
		tester.StartRecording()
		errorGen(checker, tx.pass...)
		checkResults(t, tx.sites, tester.Recording())
		// Now track the results
		template.TrackErrors(tester.Recording())
		if template.AllErrorsSeen() {
			t.Error("Prematurely saw all errors at iteration", i)
		}
	}

	// Now run the one with an error at the end
	lastErr := cases[15]
	tester.StartRecording()
	err := errorGen(checker, lastErr.pass...)
	checkResults(t, lastErr.sites, tester.Recording())
	if err == nil || err.Error() != errorExpected[4].err {
		t.Error("Didn't hit last error")
	}
	template.TrackErrors(tester.Recording())
	if !template.AllErrorsSeen() {
		t.Error("Didn't see all errors when expected")
	}

	// tests for TrackError edge cases
	tester.StartRecording()
	errorGen(checker, clean.pass...)
	allClean := tester.Recording()
	tester.StartRecording()
	errorGen(checker, cases[0].pass...)
	one := tester.Recording()

	if err := one.TrackErrors(allClean); err == nil || !strings.HasPrefix(err.Error(), "error recording has") {
		t.Error("track more than expected didn't fail")
	}

	if err := allClean.TrackErrors(allClean[2:]); err == nil || !strings.HasPrefix(err.Error(), "recording number") {
		t.Error("recording number mismatch undetected")
	}

	if !one.ErrorMatches(0, "true error") {
		t.Error("First failure does not match")
	}

	if one.ErrorMatches(4, "true error") {
		t.Error("Error matches incorrectly")
	}
	if allClean.ErrorMatches(0, "true error") {
		t.Error("Error matches incorrectly")
	}
}

func TestSimulatedFailures(t *testing.T) {
	var checker fault.FaultCheck = fault.NewChecker().SetFaulter(fault.Simple)
	tester := NewTestChecker(nil)
	defer tester.Patch(&checker).Reset()
	cases := genCases()

	for i, test := range []struct {
		tx       errorGenTest
		failAt   int
		err      error
		expected string
	}{
		{cases[31], 0, errors.New("some error"), errorExpected[0].err},
		{cases[31], 0, nil, ""},
		{cases[0], 0, nil, errorExpected[0].err},                      // A real failure
		{cases[0], 0, errors.New("some error"), errorExpected[0].err}, // A real failure
		{cases[31], 1, errors.New("some error"), errorExpected[1].err},
		{cases[31], 1, nil, ""},
		{cases[1], 1, nil, errorExpected[1].err},                      // A real failure
		{cases[1], 1, errors.New("some error"), errorExpected[1].err}, // A real failure
		{cases[31], 2, errors.New("some error"), "some error"},
		{cases[31], 2, nil, ""},
		{cases[3], 2, nil, errorExpected[2].err},                      // A real failure
		{cases[3], 2, errors.New("some error"), errorExpected[2].err}, // A real failure
		{cases[31], 3, errors.New("some error"), "some error"},
		{cases[31], 3, nil, ""},
		{cases[7], 3, nil, errorExpected[3].err},                      // A real failure
		{cases[7], 3, errors.New("some error"), errorExpected[3].err}, // A real failure
		{cases[31], 4, errors.New("some error"), "some error; output: 0"},
		{cases[31], 4, nil, ""},
		{cases[15], 4, nil, errorExpected[4].err},                      // A real failure
		{cases[15], 4, errors.New("some error"), errorExpected[4].err}, // A real failure
	} {
		tester.ResetFailures()
		tester.FailAt(test.failAt, test.err)

		tester.StartRecording()
		failed := errorGen(checker, test.tx.pass...)
		if (failed == nil && test.expected != "") || (failed != nil && failed.Error() != test.expected) {
			t.Log(test)
			t.Error(i, "Failure not as expected", test.expected, "obtained", failed)
		}
	}
}
