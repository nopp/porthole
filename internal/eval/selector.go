package eval

import (
	"net/netip"
	"strconv"
	"strings"

	"github.com/pia/porthole/internal/model"
)

// Matches reports whether labels satisfy sel.
// A selector with no matchLabels and no expressions matches everything.
// NotIn and DoesNotExist match when the key is absent. In and Exists do not.
func Matches(sel model.LabelSelector, labels map[string]string) bool {
	for k, want := range sel.MatchLabels {
		got, ok := lookup(labels, k)
		if !ok || got != want {
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
	got, ok := lookup(labels, exp.Key)
	switch exp.Operator {
	case "In":
		if !ok {
			return false
		}
		for _, v := range exp.Values {
			if got == v {
				return true
			}
		}
		return false
	case "NotIn":
		if !ok {
			return true
		}
		for _, v := range exp.Values {
			if got == v {
				return false
			}
		}
		return true
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

func lookup(labels map[string]string, key string) (string, bool) {
	if labels == nil {
		return "", false
	}
	v, ok := labels[key]
	return v, ok
}

// IPInBlock reports whether ip is inside the CIDR and outside every except range.
func IPInBlock(ip string, block model.IPBlock) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	prefix, err := netip.ParsePrefix(block.CIDR)
	if err != nil {
		return false
	}
	if !prefix.Contains(addr) {
		return false
	}
	for _, ex := range block.Except {
		p, err := netip.ParsePrefix(ex)
		if err != nil {
			continue
		}
		if p.Contains(addr) {
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
	for i := 0; i < 64; i++ {
		if IPInBlock(addr.String(), block) {
			return addr.String(), true
		}
		next, ok := nextAddr(addr)
		if !ok || !prefix.Contains(next) {
			return "", false
		}
		addr = next
	}
	return "", false
}

func nextAddr(a netip.Addr) (netip.Addr, bool) {
	b := a.AsSlice()
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			next, ok := netip.AddrFromSlice(b)
			return next, ok
		}
	}
	return netip.Addr{}, false
}

func normProto(p string) string {
	if strings.TrimSpace(p) == "" {
		return "TCP"
	}
	return strings.ToUpper(p)
}
