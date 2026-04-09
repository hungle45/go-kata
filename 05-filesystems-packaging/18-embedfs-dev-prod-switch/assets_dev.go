//go:build dev

package main

import (
	"io/fs"
	"os"
)

const Environment = "dev"

// Assets returns template and static filesystems sourced from disk
func Assets() (templates fs.FS, static fs.FS, err error) {
	return os.DirFS("templates"), os.DirFS("static"), nil
}
