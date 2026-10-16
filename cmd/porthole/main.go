package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pia/porthole/internal/eval"
	"github.com/pia/porthole/internal/load"
	"github.com/pia/porthole/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "serve":
		code = runServe(os.Args[2:])
	case "check":
		code = runCheck(os.Args[2:])
	case "explain":
		code = runExplain(os.Args[2:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage(os.Stderr)
		code = 2
	}
	os.Exit(code)
}

func usage(w *os.File) {
	fmt.Fprintf(w, `porthole — Understand your cluster's network boundaries.

Reads networking.k8s.io/v1 NetworkPolicies and answers whether one pod can reach another.
It does not send packets, and it does not understand CiliumNetworkPolicy or AdminNetworkPolicy.

  porthole serve   [--addr :8080] [--refresh 15s] [--manifests dir] [--kubeconfig path]
  porthole check   --from ns/pod (--to ns/pod | --to-ip ip) --port N [--protocol TCP] [--json]
  porthole explain --pod ns/pod [--json]

Without --manifests, the command uses the cluster (in-cluster config or kubeconfig).
check exits 0 when the flow is allowed and 1 when it is denied.
`)
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", ":8080", "listen address")
	refresh := fs.Duration("refresh", 15*time.Second, "reload interval; 0 disables it")
	manifests, kubeconfig := sourceFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	src, name, err := openSource(*kubeconfig, *manifests)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	srv := server.New(src, name, *refresh)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = srv.Refresh(ctx)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(runCtx, *addr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	from := fs.String("from", "", "source pod namespace/name")
	to := fs.String("to", "", "destination pod namespace/name")
	fromIP := fs.String("from-ip", "", "source IP, instead of --from")
	toIP := fs.String("to-ip", "", "destination IP, instead of --to")
	port := fs.Int("port", 0, "destination port")
	proto := fs.String("protocol", "TCP", "TCP, UDP, or SCTP")
	asJSON := fs.Bool("json", false, "print the verdict as JSON")
	manifests, kubeconfig := sourceFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	idx, err := loadIndex(*kubeconfig, *manifests)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	q := eval.Query{FromIP: *fromIP, ToIP: *toIP, Protocol: *proto, Port: int32(*port)}
	if *from != "" {
		q.From, err = parseRef(*from)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if *to != "" {
		q.To, err = parseRef(*to)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	res, err := idx.Check(q)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		printResult(os.Stdout, res)
	}
	if res.Allowed {
		return 0
	}
	return 1
}

func runExplain(args []string) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	pod := fs.String("pod", "", "pod namespace/name")
	asJSON := fs.Bool("json", false, "print JSON")
	manifests, kubeconfig := sourceFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ref, err := parseRef(*pod)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pass --pod namespace/name")
		return 2
	}
	idx, err := loadIndex(*kubeconfig, *manifests)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ex, err := idx.Explain(ref.Namespace, ref.Name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(ex)
		return 0
	}
	printExplain(os.Stdout, ex)
	return 0
}

func sourceFlags(fs *flag.FlagSet) (manifests, kubeconfig *string) {
	manifests = fs.String("manifests", "", "YAML/JSON directory instead of the cluster")
	kubeconfig = fs.String("kubeconfig", "", "explicit kubeconfig file")
	return manifests, kubeconfig
}

func openSource(kubeconfig, manifests string) (server.Source, string, error) {
	if manifests != "" && kubeconfig != "" {
		return nil, "", fmt.Errorf("use --manifests or --kubeconfig, not both")
	}
	if manifests != "" {
		return load.FileSource{Dir: manifests}, "manifests", nil
	}
	src, err := load.NewKubeSource(kubeconfig)
	if err != nil {
		return nil, "", err
	}
	return src, "cluster", nil
}

func loadIndex(kubeconfig, manifests string) (eval.Index, error) {
	src, _, err := openSource(kubeconfig, manifests)
	if err != nil {
		return eval.Index{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snap, err := src.Load(ctx)
	if err != nil {
		return eval.Index{}, err
	}
	idx := eval.NewIndex(snap)
	idx.Lang = "en"
	return idx, nil
}

func parseRef(s string) (eval.PodRef, error) {
	ns, name, ok := strings.Cut(s, "/")
	if !ok || ns == "" || name == "" || strings.Contains(name, "/") {
		return eval.PodRef{}, fmt.Errorf("use namespace/name, got %q", s)
	}
	return eval.PodRef{Namespace: ns, Name: name}, nil
}
