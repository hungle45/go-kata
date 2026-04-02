package main

import (
	"io/fs"
	"path/filepath"
)

func LoadConfigs(fsys fs.FS, root string) (map[string][]byte, error) {
	configs := make(map[string][]byte)
	err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".conf" {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		configs[path] = data
		return nil
	})
	return configs, err
}
