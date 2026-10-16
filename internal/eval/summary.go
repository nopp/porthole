package eval

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pia/porthole/internal/model"
)

func (idx Index) summarizeRules(pol model.Policy) []string {
	var lines []string
	if pol.Applies(model.Ingress) {
		lines = append(lines, idx.summarizeDirection("Ingress", pol.Ingress)...)
	}
	if pol.Applies(model.Egress) {
		lines = append(lines, idx.summarizeDirection("Egress", pol.Egress)...)
	}
	if len(lines) == 0 {
		lines = append(lines, idx.tr("Não aplica Ingress nem Egress.", "Does not apply to Ingress or Egress."))
	}
	return lines
}

func (idx Index) summarizeDirection(dir string, rules []model.Rule) []string {
	if len(rules) == 0 {
		if dir == "Ingress" {
			return []string{idx.tr(
				"Ingress: sem regras — nega todo o tráfego de entrada.",
				"Ingress: no rules — denies all incoming traffic.",
			)}
		}
		return []string{idx.tr(
			"Egress: sem regras — nega todo o tráfego de saída.",
			"Egress: no rules — denies all outgoing traffic.",
		)}
	}
	lines := make([]string, 0, len(rules))
	for _, rule := range rules {
		lines = append(lines, dir+": "+idx.summarizePeers(dir, rule.Peers)+", "+idx.summarizePorts(rule.Ports))
	}
	return lines
}

func (idx Index) summarizePeers(dir string, peers []model.Peer) string {
	word := idx.tr("de", "from")
	if dir == "Egress" {
		word = idx.tr("para", "to")
	}
	if len(peers) == 0 {
		return word + idx.tr(" todos os peers", " all peers")
	}
	parts := make([]string, 0, len(peers))
	for _, peer := range peers {
		parts = append(parts, idx.summarizePeer(peer))
	}
	return word + " " + strings.Join(parts, idx.tr(" ou ", " or "))
}

func (idx Index) summarizePeer(peer model.Peer) string {
	if peer.IPBlock != nil && peer.PodSelector == nil && peer.NamespaceSelector == nil {
		s := "ipBlock " + peer.IPBlock.CIDR
		if len(peer.IPBlock.Except) > 0 {
			s += idx.tr(" exceto ", " except ") + strings.Join(peer.IPBlock.Except, ", ")
		}
		return s
	}
	if peer.IPBlock != nil {
		return idx.tr("peer inválido (ipBlock junto com seletor)", "invalid peer (ipBlock combined with a selector)")
	}
	if peer.PodSelector == nil && peer.NamespaceSelector == nil {
		return idx.tr("peer vazio", "empty peer")
	}
	switch {
	case peer.NamespaceSelector != nil && peer.PodSelector != nil:
		return idx.tr(
			"pods "+idx.FormatSelector(*peer.PodSelector)+" em namespaces "+idx.FormatSelector(*peer.NamespaceSelector),
			"pods "+idx.FormatSelector(*peer.PodSelector)+" in namespaces "+idx.FormatSelector(*peer.NamespaceSelector),
		)
	case peer.NamespaceSelector != nil:
		return idx.tr(
			"todos os pods em namespaces "+idx.FormatSelector(*peer.NamespaceSelector),
			"all pods in namespaces "+idx.FormatSelector(*peer.NamespaceSelector),
		)
	default:
		return idx.tr(
			"pods "+idx.FormatSelector(*peer.PodSelector)+" neste namespace",
			"pods "+idx.FormatSelector(*peer.PodSelector)+" in this namespace",
		)
	}
}

func (idx Index) summarizePorts(ports []model.Port) string {
	if len(ports) == 0 {
		return idx.tr("todas as portas", "all ports")
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		proto := normProto(p.Protocol)
		switch {
		case p.Port == nil && p.PortName == "":
			parts = append(parts, idx.tr("todas as portas ", "all ")+proto+idx.tr("", " ports"))
		case p.PortName != "" && p.Port == nil:
			parts = append(parts, proto+"/"+p.PortName)
		case p.Port != nil && p.EndPort != nil:
			parts = append(parts, fmt.Sprintf("%s/%d-%d", proto, *p.Port, *p.EndPort))
		case p.Port != nil:
			parts = append(parts, fmt.Sprintf("%s/%d", proto, *p.Port))
		}
	}
	return strings.Join(parts, idx.tr(" ou ", " or "))
}

// FormatSelector renders a label selector. The package function is pt-BR.
func FormatSelector(sel model.LabelSelector) string {
	return formatSelector(sel, false)
}

// FormatSelector renders a label selector in the index language.
func (idx Index) FormatSelector(sel model.LabelSelector) string {
	return formatSelector(sel, idx.Lang == "en")
}

func formatSelector(sel model.LabelSelector, english bool) string {
	all := "(todos)"
	if english {
		all = "(all)"
	}
	if Universal(sel) {
		return all
	}
	var parts []string
	keys := make([]string, 0, len(sel.MatchLabels))
	for k := range sel.MatchLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+sel.MatchLabels[k])
	}
	for _, exp := range sel.MatchExpressions {
		switch exp.Operator {
		case "Exists":
			parts = append(parts, exp.Key+" exists")
		case "DoesNotExist":
			parts = append(parts, exp.Key+" does not exist")
		case "In", "NotIn", "Gt", "Lt":
			parts = append(parts, fmt.Sprintf("%s %s (%s)", exp.Key, strings.ToLower(exp.Operator), strings.Join(exp.Values, ",")))
		default:
			parts = append(parts, exp.Key+" "+exp.Operator)
		}
	}
	return strings.Join(parts, ", ")
}

func (idx Index) defaultingNote(pol model.Policy) string {
	if !pol.PolicyTypesDefaulted {
		return ""
	}
	if pol.HasEgressSection && !pol.HasIngressSection {
		return idx.tr(
			"policyTypes foi omitido. Como existe seção egress, a API também isola Ingress. Sem regras de ingress, todo o tráfego de entrada é negado. Para afetar só a saída, defina policyTypes: [Egress].",
			"policyTypes was omitted. Because an egress section exists, the API also isolates Ingress. With no ingress rules, all incoming traffic is denied. To affect only outgoing traffic, set policyTypes: [Egress].",
		)
	}
	if pol.HasEgressSection {
		return idx.tr(
			"policyTypes foi omitido. A API aplica Ingress e Egress.",
			"policyTypes was omitted. The API applies both Ingress and Egress.",
		)
	}
	return idx.tr(
		"policyTypes foi omitido. A API aplica só Ingress.",
		"policyTypes was omitted. The API applies Ingress only.",
	)
}
