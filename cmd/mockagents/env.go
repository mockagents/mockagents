package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Strict MOCKAGENTS_* environment parsing (audit M-35).
//
// Every knob used to have its own lenient parser: MOCKAGENTS_PORT=808O started
// on 8080, MOCKAGENTS_DEFAULT_RATE_PER_SEC=10rps parsed to 0 and disabled rate
// limiting, MOCKAGENTS_MULTI_TENANT=true (rather than 1) silently ran the
// control plane with auth off. A value that is set but unparsable is now a
// startup error — the operator typed something, and the safe reading of a
// typo on a security or capacity switch is "refuse to guess".

// startupEnvErrors collects problems found while computing flag defaults in
// init(), where nothing can be returned; runStart reports them before doing
// anything else.
var startupEnvErrors []error

func recordStartupEnvError(err error) {
	if err != nil {
		startupEnvErrors = append(startupEnvErrors, err)
	}
}

// envString returns the trimmed value and whether it was set to anything.
func envString(name string) (string, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	return v, v != ""
}

// envBool parses a boolean switch. Unset is (false, nil). Accepted spellings
// are 1/0, true/false, yes/no, on/off (case-insensitive); anything else is
// an error naming the variable.
func envBool(name string) (bool, error) {
	v, ok := envString(name)
	if !ok {
		return false, nil
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("%s=%q is not a boolean (use 1/0, true/false, yes/no, on/off)", name, v)
}

// envInt parses an integer within [min, max]; max <= 0 means no upper bound.
// The bool reports whether the variable was set.
func envInt(name string, min, max int) (int, bool, error) {
	v, ok := envString(name)
	if !ok {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, true, fmt.Errorf("%s=%q is not an integer", name, v)
	}
	if n < min || (max > 0 && n > max) {
		if max > 0 {
			return 0, true, fmt.Errorf("%s=%d is out of range (%d..%d)", name, n, min, max)
		}
		return 0, true, fmt.Errorf("%s=%d must be >= %d", name, n, min)
	}
	return n, true, nil
}

// envFloat parses a number that must be >= min.
func envFloat(name string, min float64) (float64, bool, error) {
	v, ok := envString(name)
	if !ok {
		return 0, false, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, true, fmt.Errorf("%s=%q is not a number", name, v)
	}
	if f < min {
		return 0, true, fmt.Errorf("%s=%v must be >= %v", name, f, min)
	}
	return f, true, nil
}

// envDuration parses a Go duration ("30m", "24h") that must be positive. A
// bare number is rejected on purpose: "24" is ambiguous, and the old parser
// silently kept the default for it.
func envDuration(name string) (time.Duration, bool, error) {
	v, ok := envString(name)
	if !ok {
		return 0, false, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, true, fmt.Errorf("%s=%q is not a duration (use a unit, e.g. 30m or 24h)", name, v)
	}
	if d <= 0 {
		return 0, true, fmt.Errorf("%s=%q must be positive", name, v)
	}
	return d, true, nil
}

// joinStartupEnvErrors returns every error recorded during init(), or nil.
func joinStartupEnvErrors() error {
	if len(startupEnvErrors) == 0 {
		return nil
	}
	return errors.Join(startupEnvErrors...)
}

// envEnabled is the lenient predecessor of envBool, kept so call sites that
// land from parallel branches keep compiling. New code should use envBool and
// surface its error; this wrapper reports an unparsable value as false.
func envEnabled(name string) bool {
	b, _ := envBool(name)
	return b
}
