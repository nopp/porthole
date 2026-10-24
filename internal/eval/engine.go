// Package eval evaluates networking.k8s.io/v1 NetworkPolicy.
// Results follow the API. Nothing is probed.
package eval

import (
	"fmt"
	"slices"
	"strings"

	"github.com/pia/porthole/internal/model"
)

// MaxGraphPods caps how many pods a focus graph will draw.
const MaxGraphPods = 48

// Index is a snapshot plus lookup maps.
// Lang "en" selects English. Anything else is Portuguese.
type Index struct {
	Snap     model.Snapshot
	Lang     string
	nsByName map[string]model.Namespace
	podByKey map[string]model.Pod
	polsByNS map[string][]model.Policy
}

// NewIndex indexes snap. Do not mutate snap afterwards.
func NewIndex(snap model.Snapshot) Index {
	idx := Index{
		Snap:     snap,
		nsByName: make(map[string]model.Namespace, len(snap.Namespaces)),
		podByKey: make(map[string]model.Pod, len(snap.Pods)),
		polsByNS: make(map[string][]model.Policy),
	}
	for _, ns := range snap.Namespaces {
		idx.nsByName[ns.Name] = ns
	}
	for _, pod := range snap.Pods {
		idx.podByKey[pod.Key()] = pod
	}
	for _, pol := range snap.Policies {
		idx.polsByNS[pol.Namespace] = append(idx.polsByNS[pol.Namespace], pol)
	}
	return idx
}

func (idx Index) tr(pt, en string) string {
	if idx.Lang == "en" {
		return en
	}
	return pt
}

// PodRef names a pod in the snapshot.
type PodRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func (r PodRef) key() string {
	return r.Namespace + "/" + r.Name
}

func (r PodRef) empty() bool {
	return r.Namespace == "" && r.Name == ""
}

// Query is one flow. Set either From or FromIP, and either To or ToIP.
type Query struct {
	From     PodRef `json:"from"`
	FromIP   string `json:"fromIP"`
	To       PodRef `json:"to"`
	ToIP     string `json:"toIP"`
	Protocol string `json:"protocol"`
	Port     int32  `json:"port"`
}

// QueryError is a bad query, not a denied flow.
type QueryError struct {
	Msg      string
	NotFound bool
}

func (e *QueryError) Error() string {
	return e.Msg
}

func badQuery(format string, args ...any) error {
	return &QueryError{Msg: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &QueryError{Msg: fmt.Sprintf(format, args...), NotFound: true}
}

// Result is the verdict for one flow.
type Result struct {
	Allowed  bool   `json:"allowed"`
	Protocol string `json:"protocol"`
	Port     int32  `json:"port"`
	From     string `json:"from"`
	To       string `json:"to"`
	SamePod  bool   `json:"samePod,omitempty"`
	Egress   *Side  `json:"egress,omitempty"`
	Ingress  *Side  `json:"ingress,omitempty"`
	Summary  string `json:"summary"`
}

// Side is the ingress or egress half of a flow.
type Side struct {
	Direction string        `json:"direction"`
	Subject   string        `json:"subject"`
	Isolated  bool          `json:"isolated"`
	Allowed   bool          `json:"allowed"`
	Reason    string        `json:"reason"`
	Policies  []PolicyTrace `json:"policies"`
}

// PolicyTrace is one policy considered on a side.
type PolicyTrace struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Selects   bool   `json:"selects"`
	Applies   bool   `json:"applies"`
	Matched   bool   `json:"matched"`
	Via       string `json:"via,omitempty"`
	Detail    string `json:"detail"`
}

// Explain is how the policies in a pod's namespace apply to it.
type Explain struct {
	Namespace       string                `json:"namespace"`
	Name            string                `json:"name"`
	Labels          map[string]string     `json:"labels"`
	IPs             []string              `json:"ips"`
	Ports           []model.ContainerPort `json:"ports"`
	HostNetwork     bool                  `json:"hostNetwork"`
	Phase           string                `json:"phase,omitempty"`
	IngressIsolated bool                  `json:"ingressIsolated"`
	EgressIsolated  bool                  `json:"egressIsolated"`
	Notes           []string              `json:"notes,omitempty"`
	Policies        []ExplainPolicy       `json:"policies"`
}

// ExplainPolicy is one policy in an Explain result.
type ExplainPolicy struct {
	Namespace            string            `json:"namespace"`
	Name                 string            `json:"name"`
	Selects              bool              `json:"selects"`
	PolicyTypes          []model.Direction `json:"policyTypes"`
	PolicyTypesDefaulted bool              `json:"policyTypesDefaulted"`
	IngressRules         int               `json:"ingressRules"`
	EgressRules          int               `json:"egressRules"`
	IsolatesIngress      bool              `json:"isolatesIngress"`
	IsolatesEgress       bool              `json:"isolatesEgress"`
	Rules                []string          `json:"rules"`
	Notes                []string          `json:"notes,omitempty"`
}

// Graph is the focus pod and the flows at one protocol and port.
type Graph struct {
	Focus     string      `json:"focus"`
	Protocol  string      `json:"protocol"`
	Port      int32       `json:"port"`
	Truncated bool        `json:"truncated"`
	Notes     []string    `json:"notes,omitempty"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
}

// GraphNode is a pod or an ipBlock.
type GraphNode struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Namespace string   `json:"namespace,omitempty"`
	Name      string   `json:"name"`
	IPs       []string `json:"ips,omitempty"`
	Focus     bool     `json:"focus,omitempty"`
}

// GraphEdge is one directed flow.
type GraphEdge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// PodView is a pod and whether ingress or egress is isolated.
type PodView struct {
	Namespace       string                `json:"namespace"`
	Name            string                `json:"name"`
	Labels          map[string]string     `json:"labels"`
	IPs             []string              `json:"ips"`
	Ports           []model.ContainerPort `json:"ports"`
	Phase           string                `json:"phase,omitempty"`
	HostNetwork     bool                  `json:"hostNetwork"`
	IngressIsolated bool                  `json:"ingressIsolated"`
	EgressIsolated  bool                  `json:"egressIsolated"`
}

// Pods lists pods. An empty namespace means all of them.
func (idx Index) Pods(namespace string) []PodView {
	out := make([]PodView, 0, len(idx.Snap.Pods))
	for _, pod := range idx.Snap.Pods {
		if namespace != "" && pod.Namespace != namespace {
			continue
		}
		in, eg := idx.isolated(pod)
		out = append(out, PodView{
			Namespace:       pod.Namespace,
			Name:            pod.Name,
			Labels:          pod.Labels,
			IPs:             pod.IPs,
			Ports:           pod.Ports,
			Phase:           pod.Phase,
			HostNetwork:     pod.HostNetwork,
			IngressIsolated: in,
			EgressIsolated:  eg,
		})
	}
	return out
}

// Explain lists policies in the pod's namespace and how each one applies.
func (idx Index) Explain(namespace, name string) (Explain, error) {
	pod, ok := idx.podByKey[namespace+"/"+name]
	if !ok {
		return Explain{}, notFound(idx.tr("pod %s/%s não encontrado", "pod %s/%s not found"), namespace, name)
	}
	in, eg := idx.isolated(pod)
	ex := Explain{
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		Labels:          pod.Labels,
		IPs:             pod.IPs,
		Ports:           pod.Ports,
		HostNetwork:     pod.HostNetwork,
		Phase:           pod.Phase,
		IngressIsolated: in,
		EgressIsolated:  eg,
		Policies:        []ExplainPolicy{},
	}
	if pod.HostNetwork {
		ex.Notes = append(ex.Notes, idx.tr(
			"Pod com hostNetwork. A avaliação usa o contrato da API; o CNI pode tratar o IP do nó de outro jeito.",
			"Pod uses hostNetwork. Evaluation follows the API contract; the CNI may treat the node IP differently.",
		))
	}
	if len(pod.IPs) == 0 {
		ex.Notes = append(ex.Notes, idx.tr(
			"Pod sem IP no snapshot. Regras ipBlock não conseguem casar com ele.",
			"Pod has no IP in the snapshot. ipBlock rules cannot match it.",
		))
	}
	for _, pol := range idx.polsByNS[pod.Namespace] {
		ex.Policies = append(ex.Policies, idx.explainPolicy(pol, pod))
	}
	return ex, nil
}

func (idx Index) explainPolicy(pol model.Policy, pod model.Pod) ExplainPolicy {
	selects := policySelects(pol, pod)
	row := ExplainPolicy{
		Namespace:            pol.Namespace,
		Name:                 pol.Name,
		Selects:              selects,
		PolicyTypes:          pol.PolicyTypes,
		PolicyTypesDefaulted: pol.PolicyTypesDefaulted,
		IngressRules:         len(pol.Ingress),
		EgressRules:          len(pol.Egress),
		IsolatesIngress:      selects && pol.Applies(model.Ingress),
		IsolatesEgress:       selects && pol.Applies(model.Egress),
		Rules:                idx.summarizeRules(pol),
	}
	if note := idx.defaultingNote(pol); note != "" {
		row.Notes = append(row.Notes, note)
	}
	if !selects {
		row.Notes = append(row.Notes, idx.tr("Não seleciona este pod.", "Does not select this pod."))
	}
	return row
}

// Check reports whether the flow is allowed.
// Pod-to-pod traffic must pass source egress and destination ingress.
// An IP endpoint has only that one side.
func (idx Index) Check(q Query) (Result, error) {
	proto, err := idx.normalizeProtocol(q.Protocol)
	if err != nil {
		return Result{}, err
	}
	if q.Port < 1 || q.Port > 65535 {
		return Result{}, badQuery("%s", idx.tr("porta deve estar entre 1 e 65535", "port must be between 1 and 65535"))
	}
	if q.From.empty() == (q.FromIP == "") {
		return Result{}, badQuery("%s", idx.tr("informe o pod de origem ou um IP de origem, não os dois", "set either the source pod or a source IP, not both"))
	}
	if q.To.empty() == (q.ToIP == "") {
		return Result{}, badQuery("%s", idx.tr("informe o pod de destino ou um IP de destino, não os dois", "set either the destination pod or a destination IP, not both"))
	}
	if q.FromIP != "" && q.ToIP != "" {
		return Result{}, badQuery("%s", idx.tr("os dois lados não podem ser IP; um lado precisa ser um pod", "both sides cannot be IPs; one side must be a pod"))
	}

	res := Result{Protocol: proto, Port: q.Port, From: q.FromIP, To: q.ToIP}

	var src *model.Pod
	if !q.From.empty() {
		pod, ok := idx.podByKey[q.From.key()]
		if !ok {
			return Result{}, notFound(idx.tr("pod %s não encontrado", "pod %s not found"), q.From.key())
		}
		src = &pod
		res.From = pod.Key()
	}

	var dst *model.Pod
	if !q.To.empty() {
		pod, ok := idx.podByKey[q.To.key()]
		if !ok {
			return Result{}, notFound(idx.tr("pod %s não encontrado", "pod %s not found"), q.To.key())
		}
		dst = &pod
		res.To = pod.Key()
	}

	if src != nil && dst != nil && src.Key() == dst.Key() {
		res.SamePod = true
		res.Allowed = true
		res.Summary = fmt.Sprintf(idx.tr(
			"Mesmo pod %s. Loopback (127.0.0.1) não passa por NetworkPolicy. Hairpin para o próprio IP depende do CNI; esta ferramenta trata como permitido.",
			"Same pod %s. Loopback (127.0.0.1) is not subject to NetworkPolicy. Hairpin to the pod's own IP depends on the CNI; this tool treats it as allowed.",
		), src.Key())
		return res, nil
	}

	if src != nil {
		side := idx.evalDirection(model.Egress, *src, dst, q.ToIP, proto, q.Port)
		res.Egress = &side
	}
	if dst != nil {
		side := idx.evalDirection(model.Ingress, *dst, src, q.FromIP, proto, q.Port)
		res.Ingress = &side
	}

	res.Allowed = (res.Egress == nil || res.Egress.Allowed) && (res.Ingress == nil || res.Ingress.Allowed)
	res.Summary = idx.summarizeVerdict(res)
	return res, nil
}

// Graph draws the focus pod. A peer pod or peer IP draws that flow.
// With neither, it draws pods in the focus namespace, pods in namespaces
// cited by a non-universal namespaceSelector, and ipBlocks on its policies.
func (idx Index) Graph(namespace, name, protocol string, port int32, peer PodRef, peerIP string) (Graph, error) {
	proto, err := idx.normalizeProtocol(protocol)
	if err != nil {
		return Graph{}, err
	}
	if port < 1 || port > 65535 {
		return Graph{}, badQuery("%s", idx.tr("porta deve estar entre 1 e 65535", "port must be between 1 and 65535"))
	}
	focus, ok := idx.podByKey[namespace+"/"+name]
	if !ok {
		return Graph{}, notFound(idx.tr("pod %s/%s não encontrado", "pod %s/%s not found"), namespace, name)
	}

	g := Graph{
		Focus:    focus.Key(),
		Protocol: proto,
		Port:     port,
		Nodes: []GraphNode{{
			ID:        focus.Key(),
			Kind:      "pod",
			Namespace: focus.Namespace,
			Name:      focus.Name,
			IPs:       focus.IPs,
			Focus:     true,
		}},
		Edges: []GraphEdge{},
	}

	if peerIP != "" || !peer.empty() {
		if err := idx.addChosenPeer(&g, focus, peer, peerIP, proto, port); err != nil {
			return Graph{}, err
		}
		return g, nil
	}

	peers, truncated, notes := idx.graphPeers(focus)
	g.Truncated = truncated
	g.Notes = notes
	for _, p := range peers {
		g.Nodes = append(g.Nodes, GraphNode{
			ID:        p.Key(),
			Kind:      "pod",
			Namespace: p.Namespace,
			Name:      p.Name,
			IPs:       p.IPs,
		})
		out, err := idx.Check(Query{From: podRef(focus), To: podRef(p), Protocol: proto, Port: port})
		if err != nil {
			return Graph{}, err
		}
		g.Edges = append(g.Edges, GraphEdge{From: focus.Key(), To: p.Key(), Allowed: out.Allowed, Reason: out.Summary})
		in, err := idx.Check(Query{From: podRef(p), To: podRef(focus), Protocol: proto, Port: port})
		if err != nil {
			return Graph{}, err
		}
		g.Edges = append(g.Edges, GraphEdge{From: p.Key(), To: focus.Key(), Allowed: in.Allowed, Reason: in.Summary})
	}

	seenCIDR := map[string]bool{}
	for _, pol := range idx.polsByNS[focus.Namespace] {
		if !policySelects(pol, focus) {
			continue
		}
		idx.addCIDREdges(&g, focus, pol, model.Egress, proto, port, seenCIDR)
		idx.addCIDREdges(&g, focus, pol, model.Ingress, proto, port, seenCIDR)
	}
	return g, nil
}

func (idx Index) addChosenPeer(g *Graph, focus model.Pod, peer PodRef, peerIP, proto string, port int32) error {
	if peerIP != "" {
		id := "ip:" + peerIP
		g.Nodes = append(g.Nodes, GraphNode{ID: id, Kind: "cidr", Name: peerIP})
		res, err := idx.Check(Query{From: podRef(focus), ToIP: peerIP, Protocol: proto, Port: port})
		if err != nil {
			return err
		}
		g.Edges = append(g.Edges, GraphEdge{From: focus.Key(), To: id, Allowed: res.Allowed, Reason: res.Summary})
		return nil
	}
	if peer.Namespace == "" || peer.Name == "" {
		return badQuery("%s", idx.tr("informe namespace e nome do pod de destino", "set both the destination namespace and pod name"))
	}
	other, ok := idx.podByKey[peer.key()]
	if !ok {
		return notFound(idx.tr("pod %s não encontrado", "pod %s not found"), peer.key())
	}
	if other.Key() == focus.Key() {
		return nil
	}
	g.Nodes = append(g.Nodes, GraphNode{
		ID:        other.Key(),
		Kind:      "pod",
		Namespace: other.Namespace,
		Name:      other.Name,
		IPs:       other.IPs,
	})
	out, err := idx.Check(Query{From: podRef(focus), To: podRef(other), Protocol: proto, Port: port})
	if err != nil {
		return err
	}
	g.Edges = append(g.Edges, GraphEdge{From: focus.Key(), To: other.Key(), Allowed: out.Allowed, Reason: out.Summary})
	return nil
}

func (idx Index) graphPeers(focus model.Pod) (peers []model.Pod, truncated bool, notes []string) {
	seen := map[string]bool{focus.Key(): true}
	add := func(p model.Pod) {
		if seen[p.Key()] {
			return
		}
		if len(peers) >= MaxGraphPods-1 {
			truncated = true
			return
		}
		seen[p.Key()] = true
		peers = append(peers, p)
	}
	for _, p := range idx.Snap.Pods {
		if p.Namespace == focus.Namespace {
			add(p)
		}
	}
	universalNote := idx.tr(
		"Há um namespaceSelector vazio (todos os namespaces). O grafo não puxa pods de fora do namespace do foco.",
		"A namespaceSelector matches every namespace. The graph does not pull pods from outside the focus namespace.",
	)
	for _, pol := range idx.polsByNS[focus.Namespace] {
		if !policySelects(pol, focus) {
			continue
		}
		for _, rules := range [][]model.Rule{pol.Ingress, pol.Egress} {
			for _, rule := range rules {
				for _, peer := range rule.Peers {
					if peer.NamespaceSelector == nil {
						continue
					}
					if Universal(*peer.NamespaceSelector) {
						if !slices.Contains(notes, universalNote) {
							notes = append(notes, universalNote)
						}
						continue
					}
					for _, p := range idx.Snap.Pods {
						if Matches(*peer.NamespaceSelector, idx.nsByName[p.Namespace].Labels) {
							add(p)
						}
					}
				}
			}
		}
	}
	if truncated {
		notes = append(notes, fmt.Sprintf(idx.tr("Grafo limitado a %d pods.", "Graph limited to %d pods."), MaxGraphPods))
	}
	return peers, truncated, notes
}

func (idx Index) addCIDREdges(g *Graph, focus model.Pod, pol model.Policy, dir model.Direction, proto string, port int32, seen map[string]bool) {
	if !pol.Applies(dir) {
		return
	}
	for _, rule := range pol.Rules(dir) {
		for _, peer := range rule.Peers {
			if peer.IPBlock == nil || peer.PodSelector != nil || peer.NamespaceSelector != nil {
				continue
			}
			id := "cidr:" + peer.IPBlock.CIDR
			if !seen[id] {
				seen[id] = true
				g.Nodes = append(g.Nodes, GraphNode{ID: id, Kind: "cidr", Name: peer.IPBlock.CIDR})
			}
			sample, ok := SampleIP(*peer.IPBlock)
			if !ok {
				continue
			}
			q := Query{From: podRef(focus), ToIP: sample, Protocol: proto, Port: port}
			from, to := focus.Key(), id
			if dir == model.Ingress {
				q = Query{FromIP: sample, To: podRef(focus), Protocol: proto, Port: port}
				from, to = id, focus.Key()
			}
			res, err := idx.Check(q)
			if err == nil && !slices.ContainsFunc(g.Edges, func(e GraphEdge) bool { return e.From == from && e.To == to }) {
				g.Edges = append(g.Edges, GraphEdge{From: from, To: to, Allowed: res.Allowed, Reason: res.Summary})
			}
		}
	}
}

func (idx Index) evalDirection(dir model.Direction, subject model.Pod, peerPod *model.Pod, peerIP, proto string, port int32) Side {
	side := Side{
		Direction: string(dir),
		Subject:   subject.Key(),
		Policies:  []PolicyTrace{},
	}
	var matched *PolicyTrace
	for _, pol := range idx.polsByNS[subject.Namespace] {
		tr := PolicyTrace{Namespace: pol.Namespace, Name: pol.Name}
		if !policySelects(pol, subject) {
			tr.Detail = idx.tr("não seleciona este pod", "does not select this pod")
			side.Policies = append(side.Policies, tr)
			continue
		}
		tr.Selects = true
		if !pol.Applies(dir) {
			tr.Detail = idx.tr("seleciona o pod, mas não se aplica a ", "selects the pod, but does not apply to ") + string(dir)
			side.Policies = append(side.Policies, tr)
			continue
		}
		tr.Applies = true
		side.Isolated = true
		ok, via := policyAllows(idx, pol, dir, peerPod, peerIP, proto, port, destPorts(dir, subject, peerPod))
		tr.Matched = ok
		tr.Via = via
		if ok {
			tr.Detail = idx.tr("permite (", "allows (") + via + ")"
			if matched == nil {
				cp := tr
				matched = &cp
			}
		} else if len(pol.Rules(dir)) == 0 {
			tr.Detail = idx.tr("isola ", "isolates ") + string(dir) + idx.tr(" e não tem regras", " and has no rules")
		} else {
			tr.Detail = fmt.Sprintf("%s%s/%d", idx.tr("nenhuma regra casa com este peer em ", "no rule matches this peer on "), proto, port)
		}
		side.Policies = append(side.Policies, tr)
	}

	peer := idx.peerLabel(peerPod, peerIP)
	if !side.Isolated {
		side.Allowed = true
		side.Reason = fmt.Sprintf(idx.tr(
			"%s não está isolado para %s. Todo tráfego nessa direção é permitido.",
			"%s is not isolated for %s. All traffic in that direction is allowed.",
		), subject.Key(), dir)
		return side
	}
	if matched != nil {
		side.Allowed = true
		side.Reason = fmt.Sprintf(idx.tr("Permitido por %s/%s: %s.", "Allowed by %s/%s: %s."), matched.Namespace, matched.Name, matched.Via)
		return side
	}
	side.Reason = fmt.Sprintf(idx.tr(
		"%s está isolado para %s e nenhuma regra permite %s em %s/%d.",
		"%s is isolated for %s and no rule allows %s on %s/%d.",
	), subject.Key(), dir, peer, proto, port)
	return side
}

func destPorts(dir model.Direction, subject model.Pod, peerPod *model.Pod) []model.ContainerPort {
	// Named ports resolve on the destination of the packet.
	if dir == model.Ingress {
		return subject.Ports
	}
	if peerPod != nil {
		return peerPod.Ports
	}
	return nil
}

func policyAllows(idx Index, pol model.Policy, dir model.Direction, peerPod *model.Pod, peerIP, proto string, port int32, ports []model.ContainerPort) (bool, string) {
	for _, rule := range pol.Rules(dir) {
		if ok, via := ruleAllows(idx, pol.Namespace, rule, peerPod, peerIP, proto, port, ports); ok {
			return true, via
		}
	}
	return false, ""
}

func ruleAllows(idx Index, policyNS string, rule model.Rule, peerPod *model.Pod, peerIP, proto string, port int32, dest []model.ContainerPort) (bool, string) {
	if !portsAllow(rule.Ports, proto, port, dest) {
		return false, ""
	}
	via := idx.tr("todos os peers", "all peers")
	if len(rule.Peers) > 0 {
		var ok bool
		for _, peer := range rule.Peers {
			if ok, via = peerMatches(idx, policyNS, peer, peerPod, peerIP); ok {
				break
			}
		}
		if !ok {
			return false, ""
		}
	}
	if len(rule.Ports) == 0 {
		return true, via + idx.tr(", todas as portas", ", all ports")
	}
	return true, fmt.Sprintf("%s, %s/%d", via, proto, port)
}

func peerMatches(idx Index, policyNS string, peer model.Peer, peerPod *model.Pod, peerIP string) (bool, string) {
	if peer.IPBlock != nil && (peer.PodSelector != nil || peer.NamespaceSelector != nil) {
		return false, ""
	}
	if peerPod != nil {
		if peer.IPBlock != nil {
			for _, ip := range peerPod.IPs {
				if IPInBlock(ip, *peer.IPBlock) {
					return true, fmt.Sprintf(idx.tr(
						"ipBlock %s casa com o IP %s do pod (depende do CNI; a API recomenda ipBlock para IPs fora do cluster)",
						"ipBlock %s matches pod IP %s (CNI-dependent; the API intends ipBlock for addresses outside the cluster)",
					), peer.IPBlock.CIDR, ip)
				}
			}
			return false, ""
		}
		if peer.PodSelector == nil && peer.NamespaceSelector == nil {
			return false, ""
		}
		if peer.NamespaceSelector != nil {
			if !Matches(*peer.NamespaceSelector, idx.nsByName[peerPod.Namespace].Labels) {
				return false, ""
			}
		} else if peerPod.Namespace != policyNS {
			return false, ""
		}
		if peer.PodSelector != nil && !Matches(*peer.PodSelector, peerPod.Labels) {
			return false, ""
		}
		switch {
		case peer.NamespaceSelector != nil && peer.PodSelector != nil:
			return true, idx.tr("namespaceSelector e podSelector", "namespaceSelector and podSelector")
		case peer.NamespaceSelector != nil:
			return true, "namespaceSelector"
		default:
			return true, idx.tr("podSelector no namespace ", "podSelector in namespace ") + policyNS
		}
	}

	if peerIP != "" && peer.IPBlock != nil && IPInBlock(peerIP, *peer.IPBlock) {
		return true, "ipBlock " + peer.IPBlock.CIDR
	}
	return false, ""
}

func portsAllow(ports []model.Port, proto string, port int32, dest []model.ContainerPort) bool {
	if len(ports) == 0 {
		return true
	}
	for _, p := range ports {
		if !strings.EqualFold(normProto(p.Protocol), proto) {
			continue
		}
		switch {
		case p.Port == nil && p.PortName == "":
			return true
		case p.Port == nil:
			if namedPort(p.PortName, proto, port, dest) {
				return true
			}
		case p.EndPort != nil:
			if port >= *p.Port && port <= *p.EndPort {
				return true
			}
		default:
			if port == *p.Port {
				return true
			}
		}
	}
	return false
}

func namedPort(name, proto string, port int32, dest []model.ContainerPort) bool {
	return slices.ContainsFunc(dest, func(cp model.ContainerPort) bool {
		return cp.Name == name && strings.EqualFold(normProto(cp.Protocol), proto) && cp.Port == port
	})
}

func policySelects(pol model.Policy, pod model.Pod) bool {
	return pol.Namespace == pod.Namespace && Matches(pol.PodSelector, pod.Labels)
}

func (idx Index) isolated(pod model.Pod) (ingress, egress bool) {
	for _, pol := range idx.polsByNS[pod.Namespace] {
		if !policySelects(pol, pod) {
			continue
		}
		if pol.Applies(model.Ingress) {
			ingress = true
		}
		if pol.Applies(model.Egress) {
			egress = true
		}
	}
	return ingress, egress
}

func (idx Index) normalizeProtocol(p string) (string, error) {
	switch proto := normProto(p); proto {
	case "TCP", "UDP", "SCTP":
		return proto, nil
	default:
		return "", badQuery(idx.tr("protocolo %q não é TCP, UDP ou SCTP", "protocol %q is not TCP, UDP, or SCTP"), p)
	}
}

func podRef(p model.Pod) PodRef {
	return PodRef{Namespace: p.Namespace, Name: p.Name}
}

func (idx Index) peerLabel(pod *model.Pod, ip string) string {
	if pod != nil {
		return pod.Key()
	}
	if ip != "" {
		return ip
	}
	return idx.tr("o peer", "the peer")
}

func (idx Index) summarizeVerdict(res Result) string {
	flow := fmt.Sprintf(idx.tr("%s → %s em %s/%d", "%s → %s on %s/%d"), res.From, res.To, res.Protocol, res.Port)
	if res.Allowed {
		return flow + idx.tr(" é permitido.", " is allowed.")
	}
	var parts []string
	if res.Egress != nil && !res.Egress.Allowed {
		parts = append(parts, idx.tr("egress de ", "egress of ")+res.Egress.Subject+": "+res.Egress.Reason)
	}
	if res.Ingress != nil && !res.Ingress.Allowed {
		parts = append(parts, idx.tr("ingress de ", "ingress of ")+res.Ingress.Subject+": "+res.Ingress.Reason)
	}
	return flow + idx.tr(" é negado. ", " is denied. ") + strings.Join(parts, " ")
}
