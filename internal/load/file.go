package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/yaml"

	"github.com/pia/porthole/internal/model"
)

// FileSource reads Pod, Namespace, and NetworkPolicy documents from a directory.
// It accepts one document, several documents split by a --- line, and List
// kinds, including the List produced by kubectl get -o yaml.
type FileSource struct {
	Dir string
}

type builder struct {
	namespaces []corev1.Namespace
	pods       []corev1.Pod
	policies   []networkingv1.NetworkPolicy
}

// Load reads .yaml, .yml, and .json files under Dir.
func (s FileSource) Load(ctx context.Context) (model.Snapshot, error) {
	info, err := os.Stat(s.Dir)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("manifests %s: %w", s.Dir, err)
	}
	if !info.IsDir() {
		return model.Snapshot{}, fmt.Errorf("manifests %s: not a directory", s.Dir)
	}
	var b builder
	err = filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != s.Dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yaml", ".yml", ".json":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, doc := range splitDocs(data) {
			if err := ingest(doc, &b); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return model.Snapshot{}, err
	}
	return SnapshotFrom(b.namespaces, b.pods, b.policies), nil
}

func ingest(doc []byte, b *builder) error {
	if len(bytes.TrimSpace(doc)) == 0 {
		return nil
	}
	var head struct {
		Kind  string            `json:"kind"`
		Items []json.RawMessage `json:"items"`
	}
	if err := yaml.Unmarshal(doc, &head); err != nil {
		return err
	}
	if strings.HasSuffix(head.Kind, "List") {
		for _, item := range head.Items {
			if err := ingest(item, b); err != nil {
				return err
			}
		}
		return nil
	}
	switch head.Kind {
	case "Pod":
		var obj corev1.Pod
		if err := yaml.Unmarshal(doc, &obj); err != nil {
			return err
		}
		b.pods = append(b.pods, obj)
	case "Namespace":
		var obj corev1.Namespace
		if err := yaml.Unmarshal(doc, &obj); err != nil {
			return err
		}
		b.namespaces = append(b.namespaces, obj)
	case "NetworkPolicy":
		var obj networkingv1.NetworkPolicy
		if err := yaml.Unmarshal(doc, &obj); err != nil {
			return err
		}
		b.policies = append(b.policies, obj)
	}
	return nil
}

func splitDocs(data []byte) [][]byte {
	var docs [][]byte
	var buf bytes.Buffer
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if bytes.Equal(bytes.TrimSpace(line), []byte("---")) {
			if buf.Len() > 0 {
				docs = append(docs, bytes.Clone(buf.Bytes()))
				buf.Reset()
			}
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if buf.Len() > 0 {
		docs = append(docs, buf.Bytes())
	}
	return docs
}
