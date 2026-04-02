/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Helm post-renderer: reads rendered manifests on stdin, replaces kthena-router-config
// data.routerConfiguration from a file path in argv[1], writes to stdout.
package main

import (
	"bytes"
	"io"
	"os"

	"sigs.k8s.io/yaml"
)

func main() {
	if len(os.Args) < 2 {
		os.Stderr.WriteString("usage: postrender_tool <routerConfiguration-file>\n")
		os.Exit(1)
	}
	cfg, err := os.ReadFile(os.Args[1])
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	out, err := patchYAMLStream(in, string(cfg))
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		os.Exit(1)
	}
}

func patchYAMLStream(in []byte, routerConfig string) ([]byte, error) {
	in = bytes.ReplaceAll(in, []byte("\r\n"), []byte("\n"))
	docs := bytes.Split(in, []byte("\n---\n"))
	var chunks [][]byte
	for _, d := range docs {
		d = bytes.TrimSpace(d)
		if len(d) == 0 {
			continue
		}
		var doc map[string]interface{}
		if err := yaml.Unmarshal(d, &doc); err != nil {
			return nil, err
		}
		if len(doc) == 0 {
			continue
		}
		if err := patchRouterConfigMap(doc, routerConfig); err != nil {
			return nil, err
		}
		chunk, err := yaml.Marshal(doc)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return in, nil
	}
	return bytes.Join(chunks, []byte("\n---\n")), nil
}

func patchRouterConfigMap(doc map[string]interface{}, routerConfig string) error {
	kind, _ := doc["kind"].(string)
	if kind != "ConfigMap" {
		return nil
	}
	meta, _ := doc["metadata"].(map[string]interface{})
	if meta == nil {
		return nil
	}
	name, _ := meta["name"].(string)
	if name != "kthena-router-config" {
		return nil
	}
	data, ok := doc["data"].(map[string]interface{})
	if !ok || data == nil {
		data = make(map[string]interface{})
		doc["data"] = data
	}
	data["routerConfiguration"] = routerConfig
	return nil
}
