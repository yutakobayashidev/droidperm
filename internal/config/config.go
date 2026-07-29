package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/yutakobayashidev/droidperm/internal/policy"
	"go.yaml.in/yaml/v3"
)

// Load parses and validates a policy from YAML.
func Load(r io.Reader) (*policy.File, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(document.Content) == 0 {
		return nil, errors.New("parse config: empty document")
	}
	if err := rejectDuplicateKeys(document.Content[0]); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var file policy.File
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	var extra any
	err = decoder.Decode(&extra)
	if !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("parse config: multiple YAML documents are not supported")
		}
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := Validate(&file); err != nil {
		return nil, err
	}
	return &file, nil
}

// LoadFile parses and validates a policy from path.
func LoadFile(path string) (*policy.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %q: %w", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	return Load(f)
}

// Validate checks the semantic constraints of a policy.
func Validate(file *policy.File) error {
	if file == nil {
		return errors.New("validate config: policy is nil")
	}
	if file.Version != policy.Version {
		return fmt.Errorf("validate config: version must be %d, got %d", policy.Version, file.Version)
	}

	for packageName, pkg := range file.Packages {
		if strings.TrimSpace(packageName) == "" {
			return errors.New("validate config: package name must not be blank")
		}
		if len(pkg.Permissions) == 0 && len(pkg.AppOps) == 0 {
			return fmt.Errorf("validate config: package %q must define permissions or appops", packageName)
		}

		for name, mode := range pkg.Permissions {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("validate config: package %q has a blank permission name", packageName)
			}
			if !policy.ValidPermissionMode(mode) {
				return fmt.Errorf("validate config: package %q permission %q has invalid mode %q", packageName, name, mode)
			}
		}
		for name, mode := range pkg.AppOps {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("validate config: package %q has a blank appop name", packageName)
			}
			if !policy.ValidAppOpMode(mode) {
				return fmt.Errorf("validate config: package %q appop %q has invalid mode %q", packageName, name, mode)
			}
		}
	}
	return nil
}

// Marshal validates a policy and returns deterministic YAML.
func Marshal(file *policy.File) ([]byte, error) {
	if err := Validate(file); err != nil {
		return nil, err
	}

	root := mappingNode()
	appendPair(root, scalarNode("version"), integerNode(file.Version))

	packages := mappingNode()
	for _, packageName := range sortedKeys(file.Packages) {
		pkg := file.Packages[packageName]
		packageNode := mappingNode()

		if len(pkg.Permissions) > 0 {
			permissions := mappingNode()
			for _, name := range sortedKeys(pkg.Permissions) {
				appendPair(permissions, scalarNode(name), scalarNode(string(pkg.Permissions[name])))
			}
			appendPair(packageNode, scalarNode("permissions"), permissions)
		}
		if len(pkg.AppOps) > 0 {
			appops := mappingNode()
			for _, name := range sortedKeys(pkg.AppOps) {
				appendPair(appops, scalarNode(name), scalarNode(string(pkg.AppOps[name])))
			}
			appendPair(packageNode, scalarNode("appops"), appops)
		}
		appendPair(packages, scalarNode(packageName), packageNode)
	}
	appendPair(root, scalarNode("packages"), packages)

	document := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	return yaml.Marshal(document)
}

func rejectDuplicateKeys(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]int, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("mapping key at line %d must be a scalar", key.Line)
			}
			if firstLine, ok := seen[key.Value]; ok {
				return fmt.Errorf("duplicate key %q at line %d (first defined at line %d)", key.Value, key.Line, firstLine)
			}
			seen[key.Value] = key.Line
		}
	}
	for _, child := range node.Content {
		if err := rejectDuplicateKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func integerNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(value)}
}

func appendPair(mapping, key, value *yaml.Node) {
	mapping.Content = append(mapping.Content, key, value)
}
