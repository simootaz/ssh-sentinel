// Package whitelist reads the two local allow lists: the hand-edited break-glass file and the
// cache of the backend whitelist written by "ssh-sentinel sync".
package whitelist

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// BreakGlass is the set of usernames in the break-glass file. It applies to both contexts.
type BreakGlass map[string]struct{}

// ErrInsecure means the break-glass file could be edited by someone other than its owner.
// Such a file is ignored: honouring it would let any local user add themselves.
var ErrInsecure = errors.New("break-glass file must be owned by root and writable by its owner only")

// ReadBreakGlass parses the file at path: one username per line, blank lines and '#' comments
// ignored. A missing file gives an empty set and no error. An insecure file gives an empty set
// and ErrInsecure.
func ReadBreakGlass(path string) (BreakGlass, error) {
	set := BreakGlass{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return set, nil
	}
	if err != nil {
		return set, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return set, err
	}
	if err := checkPrivate(info); err != nil {
		return set, err
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		set[line] = struct{}{}
	}
	return set, sc.Err()
}

// Contains reports whether user is on the list.
func (b BreakGlass) Contains(user string) bool {
	_, ok := b[user]
	return ok
}
