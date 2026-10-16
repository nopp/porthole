package load

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pia/porthole/internal/eval"
)

func TestDemoManifests(t *testing.T) {
	snap, err := (FileSource{Dir: filepath.Join("..", "..", "testdata", "demo")}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Namespaces) != 2 || len(snap.Pods) != 5 || len(snap.Policies) != 4 {
		t.Fatalf("snapshot = %d ns, %d pods, %d policies", len(snap.Namespaces), len(snap.Pods), len(snap.Policies))
	}
	idx := eval.NewIndex(snap)

	allow, err := idx.Check(eval.Query{
		From: eval.PodRef{Namespace: "app", Name: "web"},
		To:   eval.PodRef{Namespace: "data", Name: "db"},
		Port: 5432,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !allow.Allowed {
		t.Fatalf("web should reach db:5432: %s", allow.Summary)
	}

	deny, err := idx.Check(eval.Query{
		From: eval.PodRef{Namespace: "app", Name: "web"},
		To:   eval.PodRef{Namespace: "data", Name: "db"},
		Port: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deny.Allowed {
		t.Fatal("web should not reach db:80")
	}

	ext, err := idx.Check(eval.Query{
		From: eval.PodRef{Namespace: "app", Name: "web"},
		ToIP: "10.1.0.5",
		Port: 443,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ext.Allowed {
		t.Fatalf("web should reach 10.1.0.5:443: %s", ext.Summary)
	}

	ex, err := idx.Explain("app", "batch")
	if err != nil {
		t.Fatal(err)
	}
	if !ex.IngressIsolated || !ex.EgressIsolated {
		t.Fatalf("batch footgun should isolate both sides: %+v", ex)
	}
	found := false
	for _, pol := range ex.Policies {
		for _, note := range pol.Notes {
			if strings.Contains(note, "policyTypes: [Egress]") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("missing defaulting note: %+v", ex.Policies)
	}
}

func TestListAndMultidoc(t *testing.T) {
	snap, err := (FileSource{Dir: filepath.Join("..", "..", "testdata", "list")}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	idx := eval.NewIndex(snap)
	if _, err := idx.Explain("demo", "from-list"); err != nil {
		t.Fatal(err)
	}
	if len(snap.Policies) != 1 || snap.Policies[0].Name != "from-doc" {
		t.Fatalf("policies = %+v", snap.Policies)
	}
}
