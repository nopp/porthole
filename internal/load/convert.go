package load

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/pia/porthole/internal/model"
)

// SnapshotFrom converts API objects into the evaluator model and sorts them.
// policyTypes is defaulted the way the API does: Ingress always, plus Egress
// when the egress section is present.
func SnapshotFrom(namespaces []corev1.Namespace, pods []corev1.Pod, policies []networkingv1.NetworkPolicy) model.Snapshot {
	snap := model.Snapshot{
		Namespaces: make([]model.Namespace, 0, len(namespaces)),
		Pods:       make([]model.Pod, 0, len(pods)),
		Policies:   make([]model.Policy, 0, len(policies)),
	}
	for _, ns := range namespaces {
		if ns.Name != "" {
			snap.Namespaces = append(snap.Namespaces, model.Namespace{
				Name:   ns.Name,
				Labels: maps.Clone(ns.Labels),
			})
		}
	}
	for _, pod := range pods {
		if pod.Name != "" {
			snap.Pods = append(snap.Pods, convertPod(pod))
		}
	}
	for _, pol := range policies {
		if pol.Name != "" {
			snap.Policies = append(snap.Policies, convertPolicy(pol))
		}
	}
	slices.SortFunc(snap.Namespaces, func(a, b model.Namespace) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(snap.Pods, func(a, b model.Pod) int { return cmp.Compare(a.Key(), b.Key()) })
	slices.SortFunc(snap.Policies, func(a, b model.Policy) int { return cmp.Compare(a.Key(), b.Key()) })
	return snap
}

func convertPod(pod corev1.Pod) model.Pod {
	out := model.Pod{
		Namespace:   pod.Namespace,
		Name:        pod.Name,
		Labels:      maps.Clone(pod.Labels),
		Phase:       string(pod.Status.Phase),
		HostNetwork: pod.Spec.HostNetwork,
	}
	if pod.Status.PodIP != "" {
		out.IPs = append(out.IPs, pod.Status.PodIP)
	}
	for _, ip := range pod.Status.PodIPs {
		if ip.IP != "" && !slices.Contains(out.IPs, ip.IP) {
			out.IPs = append(out.IPs, ip.IP)
		}
	}
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			proto := cmp.Or(strings.ToUpper(string(p.Protocol)), "TCP")
			out.Ports = append(out.Ports, model.ContainerPort{
				Name:     p.Name,
				Port:     p.ContainerPort,
				Protocol: proto,
			})
		}
	}
	return out
}

func convertPolicy(pol networkingv1.NetworkPolicy) model.Policy {
	spec := pol.Spec
	out := model.Policy{
		Namespace:         pol.Namespace,
		Name:              pol.Name,
		PodSelector:       convertSelector(spec.PodSelector),
		HasIngressSection: spec.Ingress != nil,
		HasEgressSection:  spec.Egress != nil,
		Ingress:           convertIngress(spec.Ingress),
		Egress:            convertEgress(spec.Egress),
	}
	if len(spec.PolicyTypes) == 0 {
		out.PolicyTypesDefaulted = true
		out.PolicyTypes = []model.Direction{model.Ingress}
		if spec.Egress != nil {
			out.PolicyTypes = append(out.PolicyTypes, model.Egress)
		}
	} else {
		for _, t := range spec.PolicyTypes {
			out.PolicyTypes = append(out.PolicyTypes, model.Direction(t))
		}
	}
	return out
}

func convertIngress(rules []networkingv1.NetworkPolicyIngressRule) []model.Rule {
	out := make([]model.Rule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, model.Rule{Peers: convertPeers(rule.From), Ports: convertPorts(rule.Ports)})
	}
	return out
}

func convertEgress(rules []networkingv1.NetworkPolicyEgressRule) []model.Rule {
	out := make([]model.Rule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, model.Rule{Peers: convertPeers(rule.To), Ports: convertPorts(rule.Ports)})
	}
	return out
}

func convertPeers(peers []networkingv1.NetworkPolicyPeer) []model.Peer {
	out := make([]model.Peer, 0, len(peers))
	for _, peer := range peers {
		var p model.Peer
		if peer.PodSelector != nil {
			sel := convertSelector(*peer.PodSelector)
			p.PodSelector = &sel
		}
		if peer.NamespaceSelector != nil {
			sel := convertSelector(*peer.NamespaceSelector)
			p.NamespaceSelector = &sel
		}
		if peer.IPBlock != nil {
			p.IPBlock = &model.IPBlock{
				CIDR:   peer.IPBlock.CIDR,
				Except: slices.Clone(peer.IPBlock.Except),
			}
		}
		out = append(out, p)
	}
	return out
}

func convertPorts(ports []networkingv1.NetworkPolicyPort) []model.Port {
	out := make([]model.Port, 0, len(ports))
	for _, p := range ports {
		mp := model.Port{Protocol: "TCP"}
		if p.Protocol != nil && *p.Protocol != "" {
			mp.Protocol = strings.ToUpper(string(*p.Protocol))
		}
		if p.Port != nil {
			if p.Port.Type == intstr.String {
				mp.PortName = p.Port.StrVal
			} else {
				n := p.Port.IntVal
				mp.Port = &n
			}
		}
		if p.EndPort != nil {
			n := *p.EndPort
			mp.EndPort = &n
		}
		out = append(out, mp)
	}
	return out
}

func convertSelector(sel metav1.LabelSelector) model.LabelSelector {
	out := model.LabelSelector{MatchLabels: maps.Clone(sel.MatchLabels)}
	for _, exp := range sel.MatchExpressions {
		out.MatchExpressions = append(out.MatchExpressions, model.LabelExpression{
			Key:      exp.Key,
			Operator: string(exp.Operator),
			Values:   slices.Clone(exp.Values),
		})
	}
	return out
}
