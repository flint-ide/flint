package update

import (
	"cmp"
	"strconv"
	"strings"
)

// semver is a parsed release tag: vMAJOR.MINOR.PATCH with an optional
// pre-release suffix. Build metadata (+...) is ignored, as SemVer says.
type semver struct {
	core [3]int
	pre  []string // dot-separated pre-release identifiers; nil for a release
}

// parseVersion accepts "v1.2.3", "1.2.3" and "v1.2.3-rc.1". Anything else
// (including "dev" and "dev-<sha>" of local builds) is not a release version.
func parseVersion(s string) (semver, bool) {
	s = strings.TrimPrefix(s, "v")
	s, build, hasBuild := strings.Cut(s, "+")
	if hasBuild && !validIdentifiers(build) {
		return semver{}, false
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var v semver
	for i, p := range parts {
		n, ok := number(p)
		if !ok {
			return semver{}, false
		}
		v.core[i] = n
	}
	if hasPre {
		if !validIdentifiers(pre) {
			return semver{}, false
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

// validIdentifiers checks a dot-separated pre-release or build suffix: SemVer
// allows only non-empty ASCII letters, digits and hyphens, which also keeps
// whatever GitHub returns as a tag plain text.
func validIdentifiers(s string) bool {
	for _, id := range strings.Split(s, ".") {
		if id == "" || strings.TrimLeft(id, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-") != "" {
			return false
		}
	}
	return true
}

// number parses a non-negative decimal without sign or leading zeros.
func number(s string) (int, bool) {
	if s == "" || (len(s) > 1 && s[0] == '0') || strings.TrimLeft(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// IsRelease reports whether v is a release version (not a development build).
func IsRelease(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

// Compare orders two versions: -1 if a < b, 0 if equal, 1 if a > b. ok is false
// when either is not a release version. A pre-release sorts below its release
// (v1.2.0-rc.1 < v1.2.0).
func Compare(a, b string) (order int, ok bool) {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return 0, false
	}
	for i := range va.core {
		if c := cmp.Compare(va.core[i], vb.core[i]); c != 0 {
			return c, true
		}
	}
	return comparePre(va.pre, vb.pre), true
}

// comparePre follows SemVer §11: no suffix beats any suffix; identifiers are
// compared left to right, numbers numerically and below words; a longer list
// wins when all shared identifiers are equal.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		na, numA := number(a[i])
		nb, numB := number(b[i])
		var c int
		switch {
		case numA && numB:
			c = cmp.Compare(na, nb)
		case numA:
			c = -1
		case numB:
			c = 1
		default:
			c = strings.Compare(a[i], b[i])
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}
