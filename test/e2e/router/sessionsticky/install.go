/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package sessionsticky

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// PostRenderScriptAbs returns the absolute path to postrender.sh (Helm post-renderer wrapper).
func PostRenderScriptAbs() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	dir := filepath.Dir(filename)
	p := filepath.Join(dir, "postrender.sh")
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("post-render script %s: %w", abs, err)
	}
	return abs, nil
}
