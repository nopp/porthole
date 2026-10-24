// Package model is the snapshot passed to eval. It has no client-go types.
package model

import "slices"

// Direction is Ingress or Egress.
type Direction string

const (
	Ingress Direction = "Ingress"
	Egress  Direction = "Egress"
)

// Snapshot is namespaces, pods, and network policies.
type Snapshot struct {
	Namespaces []Namespace
	Pods       []Pod
	Policies   []Policy
}

// Namespace is a name and the labels a namespaceSelector matches.
type Namespace struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
}

// Pod is the fields NetworkPolicy evaluation uses.
type Pod struct {
	Namespace   string
	Name        string
	Labels      map[string]string
	IPs         []string
	Ports       []ContainerPort
	HostNetwork bool
	Phase       string
}

// Key is "namespace/name".
func (p Pod) Key() string {
	return p.Namespace + "/" + p.Name
}

// ContainerPort is a container port. Named policy ports match these.
type ContainerPort struct {
	Name     string `json:"name,omitempty"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// Policy is a networking.k8s.io/v1 NetworkPolicy after policyTypes defaulting.
type Policy struct {
	Namespace            string
	Name                 string
	PodSelector          LabelSelector
	PolicyTypes          []Direction
	PolicyTypesDefaulted bool
	HasIngressSection    bool
	HasEgressSection     bool
	Ingress              []Rule
	Egress               []Rule
}

// Key is "namespace/name".
func (p Policy) Key() string {
	return p.Namespace + "/" + p.Name
}

// Applies reports whether policyTypes includes dir.
func (p Policy) Applies(dir Direction) bool {
	return slices.Contains(p.PolicyTypes, dir)
}

// Rules returns the allow rules for dir.
func (p Policy) Rules(dir Direction) []Rule {
	if dir == Egress {
		return p.Egress
	}
	return p.Ingress
}

// Rule is one ingress or egress allow rule. An empty Peers slice means
// the rule names every peer. An empty Ports slice means every port.
type Rule struct {
	Peers []Peer
	Ports []Port
}

// Peer is one entry of a from or to list.
// A nil selector means the field was not set, which is different from an
// empty selector (that one matches everything).
type Peer struct {
	PodSelector       *LabelSelector
	NamespaceSelector *LabelSelector
	IPBlock           *IPBlock
}

// IPBlock is a CIDR, minus except.
type IPBlock struct {
	CIDR   string
	Except []string
}

// Port is a protocol and an optional port or named port.
// Protocol is normalized to TCP when the object omitted it.
// A nil numeric port and empty name mean "every port of this protocol".
type Port struct {
	Protocol string
	Port     *int32
	PortName string
	EndPort  *int32
}

// LabelSelector is a Kubernetes label selector. The zero value matches everything.
type LabelSelector struct {
	MatchLabels      map[string]string
	MatchExpressions []LabelExpression
}

// LabelExpression is one matchExpressions entry.
// Operator is In, NotIn, Exists, DoesNotExist, Gt, or Lt.
type LabelExpression struct {
	Key      string
	Operator string
	Values   []string
}
