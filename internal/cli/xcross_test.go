package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallXcross(t *testing.T) {
	a, _, _ := testApp(t)
	target := filepath.Join(a.Root, "bin", "xcross")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("#!/bin/sh\n"), 0755)
	link := a.xcrossLink()
	os.MkdirAll(filepath.Dir(link), 0755)
	os.WriteFile(link, []byte("upstream"), 0755)
	for i := 0; i < 2; i++ {
		if err := a.installXcross(); err != nil {
			t.Fatal(err)
		}
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Fatal(got, err)
		}
	}
	backup, err := os.ReadFile(link + ".upstream-backup")
	if err != nil || string(backup) != "upstream" {
		t.Fatal(string(backup), err)
	}
	os.Remove(link)
	os.WriteFile(link, []byte("second upstream"), 0755)
	if err := a.installXcross(); err == nil {
		t.Fatal("overwrote existing backup")
	}
	got, _ := os.ReadFile(link)
	if string(got) != "second upstream" {
		t.Fatal("lost existing binary")
	}
	os.Remove(link)
	os.Symlink("/stale/checkout", link)
	if err := a.installXcross(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Fatal(got)
	}
}

func TestBundledPath(t *testing.T) {
	a, _, _ := testApp(t)
	a.Env = append(a.Env, "PATH=/stale/upstream")
	if code := a.Main([]string{"help"}); code != 0 {
		t.Fatal(code)
	}
	if got := a.env("PATH", ""); got != filepath.Join(a.Root, "bin")+":/stale/upstream" {
		t.Fatal(got)
	}
	count := 0
	for _, entry := range a.Env {
		if strings.HasPrefix(entry, "PATH=") {
			count++
		}
	}
	if count != 1 {
		t.Fatal(a.Env)
	}
}
