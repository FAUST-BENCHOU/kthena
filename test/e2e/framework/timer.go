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
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTimer tracks timing information for e2e tests
type TestTimer struct {
	suiteName      string
	suiteStartTime time.Time
	setupStartTime time.Time
	setupDuration  time.Duration
	testDurations  map[string]time.Duration
	testStartTimes map[string]time.Time
	mu             sync.Mutex
}

// NewTestTimer creates a new TestTimer for a test suite
func NewTestTimer(suiteName string) *TestTimer {
	return &TestTimer{
		suiteName:      suiteName,
		suiteStartTime: time.Now(),
		testDurations:  make(map[string]time.Duration),
		testStartTimes: make(map[string]time.Time),
	}
}

// StartSuite marks the start of the test suite
func (tt *TestTimer) StartSuite() {
	tt.suiteStartTime = time.Now()
	fmt.Printf("\n=== [E2E TIMING] Starting test suite: %s ===\n", tt.suiteName)
}

// StartSetup marks the start of setup phase
func (tt *TestTimer) StartSetup(phase string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.setupStartTime = time.Now()
	fmt.Printf("[E2E TIMING] %s: Setup phase '%s' started\n", tt.suiteName, phase)
}

// EndSetup marks the end of setup phase
func (tt *TestTimer) EndSetup(phase string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	duration := time.Since(tt.setupStartTime)
	tt.setupDuration += duration
	fmt.Printf("[E2E TIMING] %s: Setup phase '%s' completed in %v\n", tt.suiteName, phase, duration.Round(time.Second))
}

// StartTest marks the start of a test
func (tt *TestTimer) StartTest(testName string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.testStartTimes[testName] = time.Now()
}

// EndTest marks the end of a test
func (tt *TestTimer) EndTest(testName string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if startTime, exists := tt.testStartTimes[testName]; exists {
		duration := time.Since(startTime)
		tt.testDurations[testName] = duration
		fmt.Printf("[E2E TIMING] %s: Test '%s' completed in %v\n", tt.suiteName, testName, duration.Round(time.Millisecond*100))
		delete(tt.testStartTimes, testName)
	}
}

// WrapTest wraps a test function to automatically track its timing
func (tt *TestTimer) WrapTest(testName string, testFunc func(*testing.T)) func(*testing.T) {
	return func(t *testing.T) {
		tt.StartTest(testName)
		defer tt.EndTest(testName)
		testFunc(t)
	}
}

// PrintSummary prints a summary of all timing information
func (tt *TestTimer) PrintSummary() {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	totalDuration := time.Since(tt.suiteStartTime)
	
	fmt.Printf("\n")
	fmt.Printf("╔════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║              E2E Test Suite Timing Summary                   ║\n")
	fmt.Printf("╠════════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Suite: %-50s ║\n", tt.suiteName)
	fmt.Printf("╠════════════════════════════════════════════════════════════════╣\n")
	
	// Setup timing
	fmt.Printf("║ Setup Phase: %-47s ║\n", tt.setupDuration.Round(time.Second))
	
	// Test timing
	testDuration := time.Duration(0)
	for _, duration := range tt.testDurations {
		testDuration += duration
	}
	fmt.Printf("║ Test Execution: %-45s ║\n", testDuration.Round(time.Second))
	
	// Other time (cleanup, etc.)
	otherDuration := totalDuration - tt.setupDuration - testDuration
	if otherDuration > 0 {
		fmt.Printf("║ Cleanup/Other: %-46s ║\n", otherDuration.Round(time.Second))
	}
	
	fmt.Printf("╠════════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Total Duration: %-47s ║\n", totalDuration.Round(time.Second))
	fmt.Printf("╠════════════════════════════════════════════════════════════════╣\n")
	
	// Individual test timings
	if len(tt.testDurations) > 0 {
		fmt.Printf("║ Individual Test Timings:                                    ║\n")
		fmt.Printf("╠════════════════════════════════════════════════════════════════╣\n")
		
		// Sort tests by duration (longest first) for better visibility
		type testTiming struct {
			name     string
			duration time.Duration
		}
		tests := make([]testTiming, 0, len(tt.testDurations))
		for name, duration := range tt.testDurations {
			tests = append(tests, testTiming{name: name, duration: duration})
		}
		
		// Simple bubble sort by duration (descending)
		for i := 0; i < len(tests); i++ {
			for j := i + 1; j < len(tests); j++ {
				if tests[i].duration < tests[j].duration {
					tests[i], tests[j] = tests[j], tests[i]
				}
			}
		}
		
		for _, test := range tests {
			// Truncate long test names
			displayName := test.name
			if len(displayName) > 50 {
				displayName = displayName[:47] + "..."
			}
			fmt.Printf("║   %-50s %8s ║\n", displayName, test.duration.Round(time.Millisecond*100))
		}
		fmt.Printf("╠════════════════════════════════════════════════════════════════╣\n")
	}
	
	fmt.Printf("╚════════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("\n")
	
	// Also output in a format that's easy to parse in CI
	fmt.Printf("[E2E TIMING SUMMARY] Suite=%s Setup=%v Tests=%v Total=%v\n",
		tt.suiteName,
		tt.setupDuration.Round(time.Second),
		testDuration.Round(time.Second),
		totalDuration.Round(time.Second))
}

// GetSuiteName extracts the suite name from the caller's package path
func GetSuiteName() string {
	// Get the caller's package name
	pc, file, _, ok := runtime.Caller(1)
	if !ok {
		return "unknown"
	}
	
	// Extract package name from file path
	// e.g., /path/to/test/e2e/router/e2e_test.go -> router
	parts := strings.Split(file, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "e2e" && i+1 < len(parts) {
			// Get the package name
			pkgPath := runtime.FuncForPC(pc).Name()
			// Extract package name (e.g., github.com/volcano-sh/kthena/test/e2e/router.TestMain -> router)
			pkgParts := strings.Split(pkgPath, ".")
			if len(pkgParts) > 1 {
				// Remove the function name and get the last part of the package path
				fullPkg := pkgParts[0]
				pkgParts2 := strings.Split(fullPkg, "/")
				if len(pkgParts2) > 0 {
					return pkgParts2[len(pkgParts2)-1]
				}
			}
			return parts[i+1]
		}
	}
	return "unknown"
}

// PrintCIFormat prints timing in a format suitable for CI parsing
func (tt *TestTimer) PrintCIFormat() {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	
	totalDuration := time.Since(tt.suiteStartTime)
	testDuration := time.Duration(0)
	for _, duration := range tt.testDurations {
		testDuration += duration
	}
	
	// Print in a format that CI can easily parse (e.g., GitHub Actions)
	fmt.Printf("::notice title=E2E Test Timing::Suite=%s,Setup=%v,Tests=%v,Total=%v\n",
		tt.suiteName,
		tt.setupDuration.Round(time.Second),
		testDuration.Round(time.Second),
		totalDuration.Round(time.Second))
	
	// Also print individual test timings
	for testName, duration := range tt.testDurations {
		fmt.Printf("::notice title=E2E Test Timing::Test=%s,Duration=%v\n",
			testName,
			duration.Round(time.Millisecond*100))
	}
}

// RunTests wraps testing.M.Run to track timing
func (tt *TestTimer) RunTests(m *testing.M) int {
	tt.StartSuite()
	code := m.Run()
	tt.PrintSummary()
	
	// Print CI format if running in CI
	if os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true" {
		tt.PrintCIFormat()
	}
	
	return code
}
