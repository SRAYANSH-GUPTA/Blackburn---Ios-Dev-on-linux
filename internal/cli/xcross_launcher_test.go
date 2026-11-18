package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestXcrossLauncherCache(t *testing.T) {
	source, err := os.ReadFile("../../bin/xcross")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, dir := range []string{"bin", "tools", "xcross/packages/xcross/bin", "app"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(root, "bin", "xcross")
	os.WriteFile(launcher, source, 0755)
	for _, name := range []string{"pubspec.yaml", "pubspec.lock", "packages/xcross/bin/xcross.dart"} {
		os.WriteFile(filepath.Join(root, "xcross", name), []byte("initial"), 0600)
	}
	fake := `#!/usr/bin/env bash
set -eu
if [[ "$1" == --version ]]; then echo test-sdk; exit 0; fi
if [[ "$1" == pub ]]; then exit 0; fi
printf 'compile\n' >> "$TRACE"
printf 'compiler chatter\n'
while [[ "$1" != --output ]]; do shift; done
mkdir -p "$2/bundle/bin"
printf '#!/usr/bin/env bash\nprintf "%%s|%%s\\n" "$PWD" "$*"\n' > "$2/bundle/bin/xcross"
chmod +x "$2/bundle/bin/xcross"
`
	os.WriteFile(filepath.Join(root, "tools", "dart"), []byte(fake), 0755)
	trace := filepath.Join(root, "trace")
	run := func() {
		t.Helper()
		cmd := exec.Command(launcher, "flutter", "build")
		cmd.Dir = filepath.Join(root, "app")
		cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "tools")+":"+os.Getenv("PATH"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"), "TRACE="+trace)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != cmd.Dir+"|flutter build\n" {
			t.Fatal(string(out))
		}
	}
	run()
	run()
	b, _ := os.ReadFile(trace)
	if strings.Count(string(b), "compile") != 1 {
		t.Fatal(string(b))
	}
	changed := filepath.Join(root, "xcross/packages/xcross/bin/extra.dart")
	os.WriteFile(changed, []byte("changed"), 0600)
	run()
	os.Remove(changed)
	run()
	b, _ = os.ReadFile(trace)
	if strings.Count(string(b), "compile") != 2 {
		t.Fatal(string(b))
	}
}
