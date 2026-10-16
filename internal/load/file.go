package load

import (
	"bufio"
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
		if err := ingestBytes(data, &b); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return model.Snapshot{}, err
	}
	return SnapshotFrom(b.namespaces, b.pods, b.policies), nil
}

func ingestBytes(data []byte, b *builder) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	// A JSON file is one value. YAML files may contain several documents.
	if looksLikeJSON(data) {
		return ingest(data, b)
	}
	docs, err := splitDocs(data)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if err := ingest(doc, b); err != nil {
			return err
		}
	}
	return nil
}

func looksLikeJSON(data []byte) bool {
	trim := bytes.TrimSpace(data)
	return len(trim) > 0 && (trim[0] == '{' || trim[0] == '[')
}

func decode(doc []byte, out any) error {
	if looksLikeJSON(doc) {
		return json.Unmarshal(doc, out)
	}
	return yaml.Unmarshal(doc, out)
}

func ingest(doc []byte, b *builder) error {
	var head struct {
		Kind  string            `json:"kind"`
		Items []json.RawMessage `json:"items"`
	}
	if err := decode(doc, &head); err != nil {
		return err
	}
	if head.Kind == "" {
		return nil
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
		if err := decode(doc, &obj); err != nil {
			return err
		}
		b.pods = append(b.pods, obj)
	case "Namespace":
		var obj corev1.Namespace
		if err := decode(doc, &obj); err != nil {
			return err
		}
		b.namespaces = append(b.namespaces, obj)
	case "NetworkPolicy":
		var obj networkingv1.NetworkPolicy
		if err := decode(doc, &obj); err != nil {
			return err
		}
		b.policies = append(b.policies, obj)
	}
	return nil
}

// splitDocs splits a YAML stream on lines whose only content is ---.
func splitDocs(data []byte) ([][]byte, error) {
	var docs [][]byte
	var buf []byte
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	flush := func() {
		if len(bytes.TrimSpace(buf)) > 0 {
			docs = append(docs, append([]byte(nil), buf...))
		}
		buf = buf[:0]
	}
	for sc.Scan() {
		line := sc.Bytes()
		if bytes.Equal(bytes.TrimSpace(line), []byte("---")) {
			flush()
			continue
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}
