package main

import (
	"blackburn/internal/cli"
	"os"
	"path/filepath"
)

func main() {
	root := os.Getenv("BLACKBURN_ROOT")
	if root == "" {
		exe, e := os.Executable()
		if e != nil {
			panic(e)
		}
		root = filepath.Dir(filepath.Dir(exe))
	}
	os.Exit(cli.New(root).Main(os.Args[1:]))
}
