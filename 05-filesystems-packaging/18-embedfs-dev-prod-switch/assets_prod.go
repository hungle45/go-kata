//go:build !dev

package main

import (
	"embed"
	"io/fs"
)

const Environment = "prod"

//go:embed templates
var embeddedTemplates embed.FS

//go:embed static
var embeddedStatic embed.FS

// Assets returns template and static filesystems from the embedded binary..
func Assets() (templates fs.FS, static fs.FS, err error) {
	templates, err = fs.Sub(embeddedTemplates, "templates")
	if err != nil {
		return nil, nil, err
	}

	static, err = fs.Sub(embeddedStatic, "static")
	if err != nil {
		return nil, nil, err
	}

	return templates, static, nil
}
