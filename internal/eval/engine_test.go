package eval

import (
	"strings"
	"testing"

	"github.com/pia/porthole/internal/model"
)

func labels(pairs ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return m
}

func sel(pairs ...string) model.LabelSelector {
	if len(pairs) == 0 {
		return model.LabelSelector{}
	}
	return model.LabelSelector{MatchLabels: labels(pairs...)}
}

func selPtr(pairs ...string) *model.LabelSelector {
	s := sel(pairs...)
	return &s
}

func port(proto string, n int32) model.Port {
	return model.Port{Protocol: proto, Port: &n}
}

func named(proto, name string) model.Port {
	return model.Port{Protocol: proto, PortName: name}
}

func portRange(proto string, from, to int32) model.Port {
	return model.Port{Protocol: proto, Port: &from, EndPort: &to}
}

func pod(ns, name string, lab map[string]string, ip string, ports ...model.ContainerPort) model.Pod {
	p := model.Pod{Namespace: ns, Name: name, Labels: lab, Ports: ports}
	if ip != "" {
		p.IPs = []string{ip}
	}
	return p
}

func policy(ns, name string, selectLabels map[string]string, types []model.Direction, ingress, egress []model.Rule) model.Policy {
	return model.Policy{
		Namespace:   ns,
		Name:        name,
		PodSelector: model.LabelSelector{MatchLabels: selectLabels},
		PolicyTypes: types,
		Ingress:     ingress,
		Egress:      egress,
	}
}

func baseSnap(pols ...model.Policy) model.Snapshot {
	return model.Snapshot{
		Namespaces: []model.Namespace{
			{Name: "app", Labels: labels("team", "app")},
			{Name: "data", Labels: labels("team", "data")},
			{Name: "other", Labels: labels("team", "other")},
		},
		Pods: []model.Pod{
			pod("app", "web", labels("app", "web"), "10.0.1.10", model.ContainerPort{Name: "http", Port: 8080, Protocol: "TCP"}),
			pod("app", "api", labels("app", "api"), "10.0.1.11", model.ContainerPort{Name: "http", Port: 8080, Protocol: "TCP"}),
			pod("app", "free", labels("app", "free"), "10.0.1.12"),
			pod("data", "db", labels("app", "db"), "10.0.2.20", model.ContainerPort{Name: "postgres", Port: 5432, Protocol: "TCP"}),
			pod("other", "x", labels("app", "web"), "10.0.3.30"),
		},
		Policies: pols,
	}
}

func check(t *testing.T, idx Index, fromNS, fromName, toNS, toName, proto string, portNum int32) Result {
	t.Helper()
	res, err := idx.Check(Query{
		From:     PodRef{Namespace: fromNS, Name: fromName},
		To:       PodRef{Namespace: toNS, Name: toName},
		Protocol: proto,
		Port:     portNum,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return res
}

func TestNoPoliciesAllow(t *testing.T) {
	res := check(t, NewIndex(baseSnap()), "app", "web", "data", "db", "TCP", 5432)
	if !res.Allowed || !res.Egress.Allowed || res.Egress.Isolated || res.Ingress.Isolated {
		t.Fatalf("expected open cluster, got %+v", res)
	}
}

func TestDefaultDenyIngressLeavesEgressOpen(t *testing.T) {
	pol := policy("data", "deny-in", nil, []model.Direction{model.Ingress}, nil, nil)
	res := check(t, NewIndex(baseSnap(pol)), "app", "web", "data", "db", "TCP", 5432)
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.Egress.Isolated || !res.Egress.Allowed {
		t.Fatalf("source egress should stay open: %+v", res.Egress)
	}
	if !res.Ingress.Isolated || res.Ingress.Allowed {
		t.Fatalf("dest ingress should be closed: %+v", res.Ingress)
	}
}

func TestDefaultDenyAll(t *testing.T) {
	pol := policy("app", "deny", labels("app", "web"), []model.Direction{model.Ingress, model.Egress}, nil, nil)
	idx := NewIndex(baseSnap(pol))
	res := check(t, idx, "app", "web", "data", "db", "TCP", 5432)
	if res.Allowed || !res.Egress.Isolated || res.Egress.Allowed {
		t.Fatalf("egress should be denied: %+v", res.Egress)
	}
	// db itself is not selected by the app policy, so its ingress is open.
	if res.Ingress.Isolated {
		t.Fatalf("db ingress should not be isolated by a policy in app: %+v", res.Ingress)
	}
	ex, err := idx.Explain("app", "web")
	if err != nil {
		t.Fatal(err)
	}
	if !ex.IngressIsolated || !ex.EgressIsolated {
		t.Fatalf("web should be isolated both ways: %+v", ex)
	}
	free, err := idx.Explain("app", "free")
	if err != nil {
		t.Fatal(err)
	}
	if free.IngressIsolated || free.EgressIsolated {
		t.Fatal("pod that is not selected must stay open")
	}
}

func TestEmptyPodSelectorSelectsAll(t *testing.T) {
	pol := policy("app", "all", nil, []model.Direction{model.Ingress}, nil, nil)
	ex, err := NewIndex(baseSnap(pol)).Explain("app", "free")
	if err != nil {
		t.Fatal(err)
	}
	if !ex.IngressIsolated || ex.EgressIsolated {
		t.Fatalf("empty selector should isolate every pod's ingress only: %+v", ex)
	}
}

func TestPodSelectorSameNamespaceOnly(t *testing.T) {
	pol := policy("app", "web-in", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr("app", "web")}},
		Ports: []model.Port{port("TCP", 8080)},
	}}, nil)
	idx := NewIndex(baseSnap(pol))
	// other/x has app=web but lives in another namespace, and web's ingress
	// only allows app=web in the policy namespace. other/x egress is open,
	// web ingress must deny it.
	res := check(t, idx, "other", "x", "app", "web", "TCP", 8080)
	if res.Allowed || res.Ingress.Allowed {
		t.Fatalf("podSelector must not cross namespaces: %+v", res.Ingress)
	}
	same := check(t, idx, "app", "web", "app", "web", "TCP", 8080)
	if !same.SamePod || !same.Allowed {
		t.Fatal("same pod should be allowed before policy")
	}
}

func TestAllowFromPodSelector(t *testing.T) {
	pol := policy("app", "web-in", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr("app", "api")}},
		Ports: []model.Port{port("TCP", 8080)},
	}}, nil)
	idx := NewIndex(baseSnap(pol))
	ok := check(t, idx, "app", "api", "app", "web", "TCP", 8080)
	if !ok.Allowed {
		t.Fatalf("api should reach web: %s", ok.Summary)
	}
	bad := check(t, idx, "app", "free", "app", "web", "TCP", 8080)
	if bad.Allowed {
		t.Fatal("free should not match the podSelector")
	}
	wrongPort := check(t, idx, "app", "api", "app", "web", "TCP", 80)
	if wrongPort.Allowed {
		t.Fatal("port 80 should be denied")
	}
	wrongProto := check(t, idx, "app", "api", "app", "web", "UDP", 8080)
	if wrongProto.Allowed {
		t.Fatal("UDP should be denied when the rule is TCP")
	}
}

func TestNamespaceSelectorAndPodSelector(t *testing.T) {
	ns := sel("team", "app")
	podSel := sel("app", "web")
	pol := policy("data", "db-in", labels("app", "db"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{
			NamespaceSelector: &ns,
			PodSelector:       &podSel,
		}},
		Ports: []model.Port{port("TCP", 5432)},
	}}, nil)
	idx := NewIndex(baseSnap(pol))
	if res := check(t, idx, "app", "web", "data", "db", "TCP", 5432); !res.Allowed {
		t.Fatalf("AND selector should allow web: %s", res.Summary)
	}
	if res := check(t, idx, "app", "api", "data", "db", "TCP", 5432); res.Allowed {
		t.Fatal("same namespace, wrong pod label, must be denied")
	}
	if res := check(t, idx, "other", "x", "data", "db", "TCP", 5432); res.Allowed {
		t.Fatal("label app=web in a namespace that fails namespaceSelector must be denied")
	}
}

func TestPeerListIsOR(t *testing.T) {
	pol := policy("data", "db-in", labels("app", "db"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{
			{PodSelector: selPtr("app", "missing")},
			{NamespaceSelector: selPtr("team", "app")},
		},
		Ports: []model.Port{port("TCP", 5432)},
	}}, nil)
	// Second peer has only a namespaceSelector, so it is NOT restricted to the
	// policy namespace. It ORs with the first peer.
	res := check(t, NewIndex(baseSnap(pol)), "app", "api", "data", "db", "TCP", 5432)
	if !res.Allowed {
		t.Fatalf("OR peer should allow the namespace: %s", res.Summary)
	}
	if !strings.Contains(res.Ingress.Reason, "namespaceSelector") {
		t.Fatalf("reason should name the namespaceSelector: %s", res.Ingress.Reason)
	}
}

func TestPoliciesAreAdditive(t *testing.T) {
	a := policy("app", "only-80", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr()}},
		Ports: []model.Port{port("TCP", 80)},
	}}, nil)
	b := policy("app", "also-443", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr()}},
		Ports: []model.Port{port("TCP", 443)},
	}}, nil)
	idx := NewIndex(baseSnap(a, b))
	if res := check(t, idx, "app", "api", "app", "web", "TCP", 443); !res.Allowed {
		t.Fatal("union of policies should allow 443")
	}
	if res := check(t, idx, "app", "api", "app", "web", "TCP", 22); res.Allowed {
		t.Fatal("22 is in neither policy")
	}
}

func TestIPBlockExceptAndExternal(t *testing.T) {
	block := &model.IPBlock{CIDR: "10.0.0.0/8", Except: []string{"10.1.0.0/24"}}
	pol := policy("app", "web-out", labels("app", "web"), []model.Direction{model.Egress}, nil, []model.Rule{{
		Peers: []model.Peer{{IPBlock: block}},
		Ports: []model.Port{port("TCP", 443)},
	}})
	idx := NewIndex(baseSnap(pol))
	// db IP 10.0.2.20 is inside the block and not excepted. db ingress is open.
	res := check(t, idx, "app", "web", "data", "db", "TCP", 443)
	if !res.Allowed {
		t.Fatalf("ipBlock should match the pod IP: %s", res.Summary)
	}
	if !strings.Contains(res.Egress.Reason, "depende do CNI") {
		t.Fatalf("pod IP matched via ipBlock should be flagged: %s", res.Egress.Reason)
	}
	denied, err := idx.Check(Query{From: PodRef{Namespace: "app", Name: "web"}, ToIP: "10.1.0.9", Protocol: "TCP", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	if denied.Allowed {
		t.Fatal("except range must deny")
	}
	ext, err := idx.Check(Query{From: PodRef{Namespace: "app", Name: "web"}, ToIP: "10.2.0.9", Protocol: "TCP", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	if !ext.Allowed || ext.Ingress != nil {
		t.Fatalf("external IP is egress-only: %+v", ext)
	}
	udp, err := idx.Check(Query{From: PodRef{Namespace: "app", Name: "web"}, ToIP: "10.2.0.9", Protocol: "UDP", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	if udp.Allowed {
		t.Fatal("protocol must match")
	}
}

func TestNamedPortAndEndPort(t *testing.T) {
	pol := policy("app", "web-in", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{
		{Peers: []model.Peer{{PodSelector: selPtr()}}, Ports: []model.Port{named("TCP", "http")}},
		{Peers: []model.Peer{{PodSelector: selPtr()}}, Ports: []model.Port{portRange("TCP", 9000, 9010)}},
	}, nil)
	idx := NewIndex(baseSnap(pol))
	if res := check(t, idx, "app", "api", "app", "web", "TCP", 8080); !res.Allowed {
		t.Fatalf("named port http should allow 8080: %s", res.Summary)
	}
	if res := check(t, idx, "app", "api", "app", "web", "TCP", 80); res.Allowed {
		t.Fatal("80 is not the named http port")
	}
	if res := check(t, idx, "app", "api", "app", "web", "TCP", 9000); !res.Allowed {
		t.Fatal("range start should match")
	}
	if res := check(t, idx, "app", "api", "app", "web", "TCP", 9010); !res.Allowed {
		t.Fatal("range end should match")
	}
	if res := check(t, idx, "app", "api", "app", "web", "TCP", 9011); res.Allowed {
		t.Fatal("just past endPort must fail")
	}
}

func TestOmittedPolicyTypesWithEgressAlsoIsolatesIngress(t *testing.T) {
	pol := model.Policy{
		Namespace:            "app",
		Name:                 "egress-footgun",
		PodSelector:          sel("app", "web"),
		PolicyTypes:          []model.Direction{model.Ingress, model.Egress},
		PolicyTypesDefaulted: true,
		HasEgressSection:     true,
		Egress: []model.Rule{{
			Peers: []model.Peer{{PodSelector: selPtr("app", "api")}},
			Ports: []model.Port{port("TCP", 8080)},
		}},
	}
	idx := NewIndex(baseSnap(pol))
	ex, err := idx.Explain("app", "web")
	if err != nil {
		t.Fatal(err)
	}
	if !ex.IngressIsolated || !ex.EgressIsolated {
		t.Fatal("defaulting should isolate both directions")
	}
	if len(ex.Policies) != 1 || len(ex.Policies[0].Notes) == 0 || !strings.Contains(ex.Policies[0].Notes[0], "policyTypes: [Egress]") {
		t.Fatalf("missing footgun note: %+v", ex.Policies)
	}
	// Ingress has no rules, so it denies. Egress allows only api:8080.
	in := check(t, idx, "app", "api", "app", "web", "TCP", 8080)
	if in.Allowed || in.Ingress.Allowed {
		t.Fatal("defaulted ingress with no rules must deny")
	}
}

func TestExplicitEgressOnlyDoesNotIsolateIngress(t *testing.T) {
	pol := model.Policy{
		Namespace:   "app",
		Name:        "egress-only",
		PodSelector: sel("app", "web"),
		PolicyTypes: []model.Direction{model.Egress},
		Egress: []model.Rule{{
			Peers: []model.Peer{{IPBlock: &model.IPBlock{CIDR: "192.0.2.0/24"}}},
			Ports: []model.Port{port("TCP", 443)},
		}},
	}
	idx := NewIndex(baseSnap(pol))
	ex, err := idx.Explain("app", "web")
	if err != nil {
		t.Fatal(err)
	}
	if ex.IngressIsolated || !ex.EgressIsolated {
		t.Fatalf("explicit egress-only must leave ingress open: %+v", ex)
	}
	res := check(t, idx, "app", "api", "app", "web", "TCP", 8080)
	if !res.Allowed {
		t.Fatalf("ingress should be open: %s", res.Summary)
	}
}

func TestCrossNamespaceNeedsBothSides(t *testing.T) {
	webOut := policy("app", "web-out", labels("app", "web"), []model.Direction{model.Egress}, nil, []model.Rule{{
		Peers: []model.Peer{{
			NamespaceSelector: selPtr("team", "data"),
			PodSelector:       selPtr("app", "db"),
		}},
		Ports: []model.Port{port("TCP", 5432)},
	}})
	dbInDeny := policy("data", "db-in", labels("app", "db"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr("app", "api")}},
		Ports: []model.Port{port("TCP", 5432)},
	}}, nil)
	idx := NewIndex(baseSnap(webOut, dbInDeny))
	denied := check(t, idx, "app", "web", "data", "db", "TCP", 5432)
	if denied.Allowed || !denied.Egress.Allowed || denied.Ingress.Allowed {
		t.Fatalf("egress ok and ingress deny: %+v", denied)
	}

	dbInAllow := policy("data", "db-in", labels("app", "db"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{
			NamespaceSelector: selPtr("team", "app"),
			PodSelector:       selPtr("app", "web"),
		}},
		Ports: []model.Port{port("TCP", 5432)},
	}}, nil)
	allowed := check(t, NewIndex(baseSnap(webOut, dbInAllow)), "app", "web", "data", "db", "TCP", 5432)
	if !allowed.Allowed {
		t.Fatalf("both sides should allow: %s", allowed.Summary)
	}
}

func TestEmptyFromAllowsEverySource(t *testing.T) {
	pol := policy("app", "open-port", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Ports: []model.Port{port("TCP", 8080)},
	}}, nil)
	res := check(t, NewIndex(baseSnap(pol)), "other", "x", "app", "web", "TCP", 8080)
	if !res.Allowed {
		t.Fatalf("omitted from matches every source: %s", res.Summary)
	}
}

func TestSamePodAllowedUnderDenyAll(t *testing.T) {
	pol := policy("app", "deny", labels("app", "web"), []model.Direction{model.Ingress, model.Egress}, nil, nil)
	res := check(t, NewIndex(baseSnap(pol)), "app", "web", "app", "web", "TCP", 1)
	if !res.SamePod || !res.Allowed {
		t.Fatal("same pod stays allowed")
	}
}

func TestProtocolDefaultsToTCP(t *testing.T) {
	pol := policy("app", "web-in", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr()}},
		Ports: []model.Port{{Port: port("TCP", 80).Port}},
	}}, nil)
	res, err := NewIndex(baseSnap(pol)).Check(Query{
		From: PodRef{Namespace: "app", Name: "api"},
		To:   PodRef{Namespace: "app", Name: "web"},
		Port: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allowed || res.Protocol != "TCP" {
		t.Fatalf("omitted protocol is TCP: %+v", res)
	}
}

func TestExternalSourceOnlyChecksIngress(t *testing.T) {
	pol := policy("app", "web-in", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{IPBlock: &model.IPBlock{CIDR: "203.0.113.0/24"}}},
		Ports: []model.Port{port("TCP", 8080)},
	}}, nil)
	idx := NewIndex(baseSnap(pol))
	ok, err := idx.Check(Query{FromIP: "203.0.113.10", To: PodRef{Namespace: "app", Name: "web"}, Protocol: "TCP", Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if !ok.Allowed || ok.Egress != nil {
		t.Fatalf("external source is ingress-only: %+v", ok)
	}
	no, err := idx.Check(Query{FromIP: "198.51.100.10", To: PodRef{Namespace: "app", Name: "web"}, Protocol: "TCP", Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if no.Allowed {
		t.Fatal("IP outside the block must be denied")
	}
}

func TestGraphMarksAllowAndDeny(t *testing.T) {
	pol := policy("app", "web-in", labels("app", "web"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr("app", "api")}},
		Ports: []model.Port{port("TCP", 8080)},
	}}, nil)
	g, err := NewIndex(baseSnap(pol)).Graph("app", "web", "TCP", 8080, PodRef{}, "")
	if err != nil {
		t.Fatal(err)
	}
	var apiToWeb, freeToWeb *GraphEdge
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.From == "app/api" && e.To == "app/web" {
			apiToWeb = e
		}
		if e.From == "app/free" && e.To == "app/web" {
			freeToWeb = e
		}
	}
	if apiToWeb == nil || !apiToWeb.Allowed {
		t.Fatalf("api → web should be allowed: %+v", g.Edges)
	}
	if freeToWeb == nil || freeToWeb.Allowed {
		t.Fatalf("free → web should be denied: %+v", g.Edges)
	}
}

func TestGraphFollowsSelectedPeer(t *testing.T) {
	allowEgress := policy("allow", "app-a-egress", labels("app", "app-a"), []model.Direction{model.Egress}, nil, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr("app", "app-b")}},
		Ports: []model.Port{port("TCP", 8080)},
	}})
	allowIngress := policy("allow", "app-b-ingress", labels("app", "app-b"), []model.Direction{model.Ingress}, []model.Rule{{
		Peers: []model.Peer{{PodSelector: selPtr("app", "app-a")}},
		Ports: []model.Port{port("TCP", 8080)},
	}}, nil)
	denyEgress := policy("deny", "app-a-egress-deny", labels("app", "app-a"), []model.Direction{model.Egress}, nil, []model.Rule{})
	denyIngress := policy("deny", "app-b-ingress-deny", labels("app", "app-b"), []model.Direction{model.Ingress}, []model.Rule{}, nil)
	snap := model.Snapshot{
		Namespaces: []model.Namespace{{Name: "allow"}, {Name: "deny"}},
		Pods: []model.Pod{
			pod("allow", "app-a", labels("app", "app-a"), "10.0.1.1"),
			pod("allow", "app-b", labels("app", "app-b"), "10.0.1.2"),
			pod("deny", "app-a", labels("app", "app-a"), "10.0.2.1"),
			pod("deny", "app-b", labels("app", "app-b"), "10.0.2.2"),
		},
		Policies: []model.Policy{allowEgress, allowIngress, denyEgress, denyIngress},
	}
	idx := NewIndex(snap)
	idx.Lang = "en"

	g, err := idx.Graph("allow", "app-a", "TCP", 8080, PodRef{Namespace: "deny", Name: "app-b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("selected peer only: %+v", g.Nodes)
	}
	if len(g.Edges) != 1 || g.Edges[0].From != "allow/app-a" || g.Edges[0].To != "deny/app-b" || g.Edges[0].Allowed {
		t.Fatalf("allow/app-a → deny/app-b must be the only edge, and denied: %+v", g.Edges)
	}

	same, err := idx.Graph("allow", "app-a", "TCP", 8080, PodRef{Namespace: "allow", Name: "app-b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var allowed bool
	for _, e := range same.Edges {
		if e.From == "allow/app-a" && e.To == "allow/app-b" {
			allowed = e.Allowed
		}
	}
	if !allowed {
		t.Fatalf("allow/app-a → allow/app-b must be allowed: %+v", same.Edges)
	}
}

func TestHostNetworkNote(t *testing.T) {
	snap := baseSnap()
	snap.Pods[0].HostNetwork = true
	ex, err := NewIndex(snap).Explain("app", "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Notes) == 0 || !strings.Contains(ex.Notes[0], "hostNetwork") {
		t.Fatalf("missing hostNetwork note: %+v", ex.Notes)
	}
}

func TestEnglishText(t *testing.T) {
	idx := NewIndex(baseSnap())
	idx.Lang = "en"
	res := check(t, idx, "app", "web", "data", "db", "TCP", 5432)
	if !strings.Contains(res.Summary, "is allowed") {
		t.Fatalf("english summary: %s", res.Summary)
	}
	ex, err := idx.Explain("app", "free")
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Policies) != 0 {
		t.Fatal("unexpected policies")
	}
}

func TestBadQuery(t *testing.T) {
	idx := NewIndex(baseSnap())
	_, err := idx.Check(Query{From: PodRef{Namespace: "app", Name: "web"}, To: PodRef{Namespace: "app", Name: "api"}, Port: 0})
	if err == nil {
		t.Fatal("expected port error")
	}
	_, err = idx.Check(Query{From: PodRef{Namespace: "no", Name: "pe"}, To: PodRef{Namespace: "app", Name: "web"}, Port: 80})
	if err == nil {
		t.Fatal("expected missing pod error")
	}
}
