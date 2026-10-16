# Scenarios: app-a → app-b

Two separate namespaces. You can apply both at the same time.

```bash
kubectl apply -f examples/allow.yaml
kubectl apply -f examples/deny.yaml
go run ./cmd/porthole serve
```

Wait until the pods are Running and open http://localhost:8080. The image is `pause`: Porthole does not send packets, it only reads the objects. This `serve` uses the kubeconfig on your machine. The Deployment is not required.

| Namespace | Flow | Port | Result |
| --- | --- | --- | --- |
| `porthole-allow` | `app-a` → `app-b` | TCP/8080 | allowed |
| `porthole-allow` | `app-a` → `app-b` | TCP/80 | denied |
| `porthole-deny` | `app-a` → `app-b` | TCP/8080 | denied |

In the allowed scenario, egress from `app-a` and ingress to `app-b` allow only TCP/8080 between them. In the denied scenario, `app-a` has no egress rule and `app-b` accepts no ingress.

In the UI, simulate `porthole-allow/app-a` to `porthole-allow/app-b` on port 8080, and the same pair in `porthole-deny`.

From the CLI, without installing anything on the cluster:

```bash
go run ./cmd/porthole check --manifests examples \
  --from porthole-allow/app-a --to porthole-allow/app-b --port 8080

go run ./cmd/porthole check --manifests examples \
  --from porthole-deny/app-a --to porthole-deny/app-b --port 8080
```

The first exits 0. The second exits 1.

To remove them from the cluster:

```bash
kubectl delete -f examples/allow.yaml
kubectl delete -f examples/deny.yaml
```
