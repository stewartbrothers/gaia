package forgejo

import "github.com/stewartbrothers/gaia/core/exitcode"

// errIssueType refuses an Issue type: it's an Aikestra concept (#404).
// Refused rather than ignored so a --type filter never returns the
// wrong Issues.
var errIssueType = exitcode.Errorf(exitcode.NotImplemented,
	"the forgejo provider has no issue types; --type is Aikestra-only")
