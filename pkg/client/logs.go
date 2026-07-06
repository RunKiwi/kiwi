package client

import "strings"

// logDelta returns the portion of curr that follows prev when curr grows by
// appending. If curr is not an extension of prev (logs were rewritten), it
// returns curr in full.
func logDelta(prev, curr string) string {
	if strings.HasPrefix(curr, prev) {
		return curr[len(prev):]
	}
	return curr
}
