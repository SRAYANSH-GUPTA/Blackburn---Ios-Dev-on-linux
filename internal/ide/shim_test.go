package ide

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Exercise the actual generated source and its UTF-8 framing against a fake
// xcross process. No simulator, Dart packages or network are involved.
func TestDartShimDAPBridge(t *testing.T) {
	dart, err := exec.LookPath("dart")
	if err != nil {
		t.Skip("Dart SDK unavailable")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python unavailable")
	}
	dir := t.TempDir()
	Generate(dir, Options{"headless", "macbox", "x86_64"})
	runtime := filepath.Join(dir, ".dart_tool/blackburn")
	os.MkdirAll(runtime, 0700)
	os.WriteFile(filepath.Join(runtime, "launch.json"), []byte(`{"device":"SIM-LIVE","environment":{"XCROSS_SIMULATOR_HOST":"macbox","XCROSS_SIMULATOR_ARCH":"x86_64"}}`), 0600)
	fake := `#!/usr/bin/env python3
import sys,json
assert sys.argv[1:] == ['flutter','dap']
def read():
    header=sys.stdin.buffer.readline()
    n=int(header.split(b':')[1])
    assert sys.stdin.buffer.readline()==b'\r\n'
    return json.loads(sys.stdin.buffer.read(n))
def send(obj):
    body=json.dumps(obj,ensure_ascii=False).encode()
    frame=b'Content-Length: '+str(len(body)).encode()+b'\r\n\r\n'+body
    for i in range(0,len(frame),3):
        sys.stdout.buffer.write(frame[i:i+3]);sys.stdout.buffer.flush()
m=read()
assert m['arguments']['args'][1]=='SIM-LIVE'
assert m['arguments']['env']['XCROSS_SIMULATOR_HOST']=='macbox'
assert m['arguments']['program']=='lib/é.dart'
send({'type':'event','event':'output','body':{'output':'vm-serv'}})
send({'type':'event','event':'output','body':{'output':'ice: ws://127.0.0.1:8181/auth=/ws\n'}})
send({'type':'response','success':True,'body':m['arguments']})
read()
`
	os.WriteFile(filepath.Join(dir, "xcross"), []byte(fake), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dart, filepath.Join(dir, ".vscode", ShimName), "debug_adapter")
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, _ := cmd.StdinPipe()
	output, _ := cmd.StdoutPipe()
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	send := func(value any) {
		t.Helper()
		body, _ := json.Marshal(value)
		frame := append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
		for _, b := range frame {
			if _, err := input.Write([]byte{b}); err != nil {
				t.Fatal(err)
			}
		}
	}
	send(map[string]any{"command": "launch", "arguments": map[string]any{"program": "lib/é.dart", "env": map[string]string{"XCROSS": "true"}, "args": []string{"--udid", UDIDToken}}})
	reader := bufio.NewReader(output)
	for i := 0; i < 3; i++ {
		header, err := reader.ReadString('\n')
		if err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatal(err, stderr.String())
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "Content-Length:")))
		if err != nil {
			t.Fatal(header)
		}
		reader.ReadString('\n')
		body := make([]byte, n)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		if !json.Valid(body) {
			t.Fatal(string(body))
		}
	}
	send(map[string]any{"command": "disconnect"})
	if err := cmd.Wait(); err != nil {
		t.Fatal(err, stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(runtime, "xcross.log"))
	if err != nil || len(DiscoverServices(string(b))) != 1 {
		t.Fatal(string(b), err)
	}
}
