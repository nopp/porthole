package eval

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/pia/porthole/internal/model"
)

// Matches reports whether labels satisfy sel.
// A selector with no matchLabels and no expressions matches everything.
// NotIn and DoesNotExist match when the key is absent. In and Exists do not.
func Matches(sel model.LabelSelector, labels map[string]string) bool {
	for k, want := range sel.MatchLabels {
		if got, ok := labels[k]; !ok || got != want {
			return false
		}
	}
	for _, exp := range sel.MatchExpressions {
		if !matchExpr(exp, labels) {
			return false
		}
	}
	return true
}

// Universal reports whether sel matches every object.
func Universal(sel model.LabelSelector) bool {
	return len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0
}

func matchExpr(exp model.LabelExpression, labels map[string]string) bool {
	got, ok := labels[exp.Key]
	switch exp.Operator {
	case "In":
		return ok && slices.Contains(exp.Values, got)
	case "NotIn":
		return !ok || !slices.Contains(exp.Values, got)
	case "Exists":
		return ok
	case "DoesNotExist":
		return !ok
	case "Gt", "Lt":
		if !ok || len(exp.Values) != 1 {
			return false
		}
		have, err1 := strconv.Atoi(got)
		want, err2 := strconv.Atoi(exp.Values[0])
		if err1 != nil || err2 != nil {
			return false
		}
		if exp.Operator == "Gt" {
			return have > want
		}
		return have < want
	default:
		return false
	}
}

// IPInBlock reports whether ip is inside the CIDR and outside every except range.
func IPInBlock(ip string, block model.IPBlock) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	prefix, err := netip.ParsePrefix(block.CIDR)
	if err != nil || !prefix.Contains(addr) {
		return false
	}
	for _, ex := range block.Except {
		if p, err := netip.ParsePrefix(ex); err == nil && p.Contains(addr) {
			return false
		}
	}
	return true
}

// SampleIP returns an address inside the block that is not excluded.
// It walks forward from the prefix base a bounded number of steps.
func SampleIP(block model.IPBlock) (string, bool) {
	prefix, err := netip.ParsePrefix(block.CIDR)
	if err != nil {
		return "", false
	}
	addr := prefix.Addr()
	for range 64 {
		if IPInBlock(addr.String(), block) {
			return addr.String(), true
		}
		addr = addr.Next()
		if !addr.IsValid() || !prefix.Contains(addr) {
			return "", false
		}
	}
	return "", false
}

func normProto(p string) string {
	return cmp.Or(strings.ToUpper(strings.TrimSpace(p)), "TCP")
}
