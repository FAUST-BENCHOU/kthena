/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package framework

import (
	"runtime"
	"strings"
	"testing"
)

var (
	globalTimer *TestTimer
)

// SetGlobalTimer sets the global test timer for the current suite
func SetGlobalTimer(timer *TestTimer) {
	globalTimer = timer
}

// GetGlobalTimer returns the global test timer
func GetGlobalTimer() *TestTimer {
	return globalTimer
}

// TrackTest wraps a test function to automatically track its timing
// Usage: func TestSomething(t *testing.T) { TrackTest(t, func(t *testing.T) { ... }) }
func TrackTest(t *testing.T, testFunc func(*testing.T)) {
	if globalTimer == nil {
		// If no timer is set, just run the test
		testFunc(t)
		return
	}

	// Get test name from the caller
	testName := getTestName()
	globalTimer.StartTest(testName)
	defer globalTimer.EndTest(testName)

	testFunc(t)
}

// getTestName extracts the test name from the call stack
func getTestName() string {
	// Walk up the call stack to find the test function
	pc := make([]uintptr, 10)
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])

	for {
		frame, more := frames.Next()
		if !more {
			break
		}

		// Look for functions that start with "Test"
		funcName := frame.Function
		parts := strings.Split(funcName, ".")
		if len(parts) > 0 {
			lastPart := parts[len(parts)-1]
			if strings.HasPrefix(lastPart, "Test") {
				return lastPart
			}
		}
	}

	return "unknown"
}
