package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/pia/porthole/internal/eval"
)

func printResult(w io.Writer, res eval.Result) {
	fmt.Fprintln(w, res.Summary)
	if res.SamePod {
		return
	}
	printSide(w, res.Egress)
	printSide(w, res.Ingress)
}

func printSide(w io.Writer, side *eval.Side) {
	if side == nil {
		return
	}
	state := "allowed"
	if !side.Allowed {
		state = "denied"
	}
	iso := "not isolated"
	if side.Isolated {
		iso = "isolated"
	}
	fmt.Fprintf(w, "\n%s of %s: %s, %s\n  %s\n", side.Direction, side.Subject, iso, state, side.Reason)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  POLICY\tSELECTS\tAPPLIES\tDETAIL")
	for _, pol := range side.Policies {
		fmt.Fprintf(tw, "  %s/%s\t%s\t%s\t%s\n",
			pol.Namespace, pol.Name, yesNo(pol.Selects), yesNo(pol.Applies), pol.Detail)
	}
	_ = tw.Flush()
}

func printExplain(w io.Writer, ex eval.Explain) {
	fmt.Fprintf(w, "Pod %s/%s\n", ex.Namespace, ex.Name)
	if len(ex.IPs) > 0 {
		fmt.Fprintf(w, "IPs: %s\n", strings.Join(ex.IPs, ", "))
	}
	if len(ex.Labels) > 0 {
		fmt.Fprintf(w, "Labels: %s\n", formatLabels(ex.Labels))
	}
	fmt.Fprintf(w, "Ingress isolated: %s\nEgress isolated: %s\n", yesNo(ex.IngressIsolated), yesNo(ex.EgressIsolated))
	for _, note := range ex.Notes {
		fmt.Fprintf(w, "Note: %s\n", note)
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "POLICY\tSELECTS\tISOLATES\tRULES")
	for _, pol := range ex.Policies {
		isola := joinDirs(pol.IsolatesIngress, pol.IsolatesEgress)
		rules := strings.Join(pol.Rules, " | ")
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\n", pol.Namespace, pol.Name, yesNo(pol.Selects), isola, rules)
	}
	_ = tw.Flush()
	for _, pol := range ex.Policies {
		for _, note := range pol.Notes {
			fmt.Fprintf(w, "\n%s/%s: %s\n", pol.Namespace, pol.Name, note)
		}
	}
}

func formatLabels(labels map[string]string) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ", ")
}

func joinDirs(ingress, egress bool) string {
	switch {
	case ingress && egress:
		return "Ingress,Egress"
	case ingress:
		return "Ingress"
	case egress:
		return "Egress"
	default:
		return "—"
	}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
