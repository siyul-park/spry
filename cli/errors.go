package cli

import (
	"errors"
	"regexp"
	"strings"

	"github.com/spf13/pflag"
)

// UsageError is a command line error. It makes spry print Message, and the
// usage reminder unless NoUsage is set, then exit with status 2.
type UsageError struct {
	Message string
	NoUsage bool
}

const (
	hashMessage = "--check-hash-based-pycs must be one of 'default', 'always', or 'never'"
	hashMissing = "Argument expected for the --check-hash-based-pycs options"
)

var (
	unknownShortRe = regexp.MustCompile(`^unknown shorthand flag: '(.+)' in -`)
	missingShortRe = regexp.MustCompile(`^flag needs an argument: '(.+)' in -`)
)

func (e *UsageError) Error() string {
	return e.Message
}

// usageFromFlagError turns a pflag error into CPython's message. It depends on
// pflag's error text, so pflag stays pinned in go.mod and every translation is
// asserted in the tests.
func usageFromFlagError(err error) error {
	if errors.Is(err, pflag.ErrHelp) {
		// pflag answers an unregistered -h with ErrHelp. Long forms never get
		// here: splitArgs rejects unregistered long options first.
		return &UsageError{Message: "Unknown option: -h"}
	}
	text := err.Error()
	switch {
	case unknownShortRe.MatchString(text):
		return &UsageError{Message: "Unknown option: -" + unknownShortRe.FindStringSubmatch(text)[1]}
	case missingShortRe.MatchString(text):
		return &UsageError{Message: "Argument expected for the -" + missingShortRe.FindStringSubmatch(text)[1] + " option"}
	case strings.HasPrefix(text, "flag needs an argument: --"+checkHashFlag):
		return &UsageError{Message: hashMissing}
	case strings.Contains(text, hashMessage):
		return &UsageError{Message: hashMessage}
	}
	return &UsageError{Message: text}
}
