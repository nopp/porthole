package load

import (
	"context"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/pia/porthole/internal/model"
)

// KubeSource lists Namespaces, Pods, and NetworkPolicies from a cluster.
// The client needs get, list, and watch on those three resources.
type KubeSource struct {
	client kubernetes.Interface
}

// NewKubeSource builds a client from an explicit kubeconfig path, in-cluster
// config, or the default kubeconfig loading rules, in that order.
func NewKubeSource(kubeconfig string) (*KubeSource, error) {
	cfg, err := restConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("could not configure the Kubernetes client: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	return &KubeSource{client: client}, nil
}

func restConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return rest.InClusterConfig()
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// Load lists namespaces, pods, and network policies.
func (s *KubeSource) Load(ctx context.Context) (model.Snapshot, error) {
	namespaces, err := s.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("list namespaces: %w", err)
	}
	pods, err := s.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("list pods: %w", err)
	}
	policies, err := s.client.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("list networkpolicies: %w", err)
	}
	return SnapshotFrom(namespaces.Items, pods.Items, policies.Items), nil
}
