package appliance

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func golden(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "vm.toml"), []byte("name='mac'\ndisk='disk.qcow2'\n[q35]\nopencore='opencore.qcow2'\nfirmware_code='OVMF_CODE.fd'\nfirmware_vars='OVMF_VARS.fd'\n"))
	for _, n := range []string{"opencore.qcow2", "OVMF_CODE.fd", "OVMF_VARS.fd"} {
		write(t, filepath.Join(dir, n), []byte(n))
	}
	disk := make([]byte, 4*block+17)
	copy(disk[2*block:], "disk data")
	write(t, filepath.Join(dir, "disk.qcow2"), disk)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	write(t, cfg, []byte("schema_version=1\n"))
	return dir, cfg
}
func TestSparseRoundTrip(t *testing.T) {
	for _, compression := range []string{"none", "zstd"} {
		t.Run(compression, func(t *testing.T) {
			dir, cfg := golden(t)
			out := filepath.Join(t.TempDir(), "vm.appliance")
			if e := Export(dir, cfg, out, compression); e != nil {
				t.Fatal(e)
			}
			s, _ := os.Stat(out)
			if s.Size() > 2*block {
				t.Fatal("zero blocks not omitted", s.Size())
			}
			dest := t.TempDir()
			m, e := Extract(out, dest)
			if e != nil {
				t.Fatal(e)
			}
			if len(m.Files) != 6 {
				t.Fatal(m)
			}
			for _, f := range m.Files {
				if f.Name == "blackburn.toml" {
					continue
				}
				a, _ := os.ReadFile(filepath.Join(dir, f.Name))
				b, _ := os.ReadFile(filepath.Join(dest, f.Name))
				if !bytes.Equal(a, b) {
					t.Fatal(f.Name)
				}
			}
			if e = Export(dir, cfg, out, compression); e == nil {
				t.Fatal("overwrote existing output")
			}
		})
	}
}
func TestManifestValidation(t *testing.T) {
	hash := strings.Repeat("0", 64)
	valid := Manifest{1, "x86_64", []File{{"vm.toml", 1, hash}, {"blackburn.toml", 1, hash}}}
	if e := valid.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"../escape", "/tmp/foo", "..", "a/b", "a\\b", ""} {
		m := valid
		m.Files = append([]File{}, valid.Files...)
		m.Files[0].Name = name
		if e := m.Validate(); e == nil {
			t.Fatal(name)
		}
	}
	for _, mutate := range []func(*Manifest){func(m *Manifest) { m.Version = 2 }, func(m *Manifest) { m.Architecture = "arm64" }, func(m *Manifest) { m.Files[0].Size = -1 }, func(m *Manifest) { m.Files[0].SHA256 = "bad" }, func(m *Manifest) { m.Files[1] = m.Files[0] }} {
		m := valid
		m.Files = append([]File{}, valid.Files...)
		mutate(&m)
		if e := m.Validate(); e == nil {
			t.Fatal(m)
		}
	}
}
func TestCorruptionAndTruncation(t *testing.T) {
	dir, cfg := golden(t)
	out := filepath.Join(t.TempDir(), "vm")
	if e := Export(dir, cfg, out, "none"); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(out)
	for _, cut := range []int{0, len(magic), len(data) - 1} {
		p := filepath.Join(t.TempDir(), "bad")
		write(t, p, data[:cut])
		if _, e := Extract(p, t.TempDir()); e == nil {
			t.Fatal("accepted truncated stream", cut)
		}
	}
	bad := bytes.Replace(data, []byte("disk data"), []byte("disk BAD!"), 1)
	write(t, out, bad)
	if _, e := Extract(out, t.TempDir()); e == nil {
		t.Fatal("accepted corruption")
	}
}
func TestRejectBackingAndExternalReferences(t *testing.T) {
	dir, cfg := golden(t)
	header := make([]byte, 20)
	copy(header, "QFI\xfb")
	binary.BigEndian.PutUint32(header[16:], 5)
	write(t, filepath.Join(dir, "disk.qcow2"), header)
	if e := Export(dir, cfg, filepath.Join(t.TempDir(), "out"), "none"); e == nil {
		t.Fatal("backing chain accepted")
	}
	for _, s := range []string{"disk='/outside'", "disk='../escape'"} {
		if _, e := References([]byte(s)); e == nil {
			t.Fatal(s)
		}
	}
	os.Remove(filepath.Join(dir, "disk.qcow2"))
	if e := os.Symlink(cfg, filepath.Join(dir, "disk.qcow2")); e != nil {
		t.Fatal(e)
	}
	if e := Export(dir, cfg, filepath.Join(t.TempDir(), "out"), "none"); e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestMalformedExtents(t *testing.T) {
	dir, cfg := golden(t)
	out := filepath.Join(t.TempDir(), "out")
	if e := Export(dir, cfg, out, "none"); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(out)
	size := binary.LittleEndian.Uint32(data[len(magic):])
	start := len(magic) + 4 + int(size)
	for _, kind := range []string{"offset", "length", "terminator", "trailing"} {
		b := append([]byte{}, data...)
		switch kind {
		case "offset":
			binary.LittleEndian.PutUint64(b[start:], 1<<63)
		case "length":
			binary.LittleEndian.PutUint32(b[start+8:], block+1)
		case "terminator":
			binary.LittleEndian.PutUint32(b[start+8:], 0)
		case "trailing":
			b = append(b, 1)
		}
		write(t, out, b)
		if _, e := Extract(out, t.TempDir()); e == nil {
			t.Fatal(kind)
		}
	}
}
func TestOversizedManifest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad")
	var b bytes.Buffer
	b.WriteString(magic)
	binary.Write(&b, binary.LittleEndian, uint32(1<<22))
	write(t, p, b.Bytes())
	if _, e := Extract(p, t.TempDir()); e == nil {
		t.Fatal("accepted huge manifest")
	}
	m := Manifest{Version: 9}
	raw, _ := json.Marshal(m)
	b.Reset()
	b.WriteString(magic)
	binary.Write(&b, binary.LittleEndian, uint32(len(raw)))
	b.Write(raw)
	write(t, p, b.Bytes())
	if _, e := Extract(p, t.TempDir()); e == nil {
		t.Fatal("accepted version")
	}
}
