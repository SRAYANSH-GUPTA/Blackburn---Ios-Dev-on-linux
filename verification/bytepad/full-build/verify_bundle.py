"""Read-only bundle, Mach-O, dependency and CodeDirectory page-hash inspection."""
import hashlib
import json
import pathlib
import plistlib
import struct
import subprocess
import sys

bundle = pathlib.Path(sys.argv[1]).resolve()
info = plistlib.loads((bundle / 'Info.plist').read_bytes())
result = {'bundle': str(bundle), 'info': info, 'binaries': [], 'plists': [], 'errors': []}
errors = result['errors']
for p in sorted(bundle.rglob('*.plist')):
    try:
        plistlib.loads(p.read_bytes())
        result['plists'].append(str(p.relative_to(bundle)))
    except Exception as e:
        errors.append(f'{p}: {e}')
assert (bundle / info['CFBundleExecutable']).is_file()

def inspect_slice(data, arch):
    u32 = lambda off: struct.unpack_from('<I', data, off)[0]
    be32 = lambda off: struct.unpack_from('>I', data, off)[0]
    entry = {'architecture': arch, 'dependencies': [], 'rpaths': [], 'signatures': [], 'platforms': []}
    pos = 32
    for _ in range(u32(16)):
        cmd, size = u32(pos), u32(pos + 4)
        assert size >= 8 and pos + size <= len(data)
        if cmd in (0xc, 0x80000018, 0x8000001f, 0x80000023):
            off = pos + u32(pos + 8)
            entry['dependencies'].append(data[off:pos+size].split(b'\0')[0].decode())
        if cmd == 0x8000001c:
            off = pos + u32(pos + 8)
            entry['rpaths'].append(data[off:pos+size].split(b'\0')[0].decode())
        if cmd == 0x32:
            entry['platforms'].append({'platform': u32(pos+8), 'minos': u32(pos+12), 'sdk': u32(pos+16)})
        if cmd == 0x1d:
            start, length = u32(pos+8), u32(pos+12)
            assert start + length <= len(data)
            assert be32(start) == 0xfade0cc0
            for i in range(be32(start+8)):
                slot, offset = struct.unpack_from('>II', data, start+12+i*8)
                cd = start+offset
                if be32(cd) != 0xfade0c02:
                    continue
                flags, hoff, ident, special, slots, limit = struct.unpack_from('>6I', data, cd+12)
                hsize, htype, _, shift = data[cd+36:cd+40]
                algo = {1:'sha1', 2:'sha256', 3:'sha256', 4:'sha384'}[htype]
                page = 1 << shift if shift else limit
                bad = []
                for n in range(slots):
                    digest = hashlib.new(algo, data[n*page:min((n+1)*page,limit)]).digest()[:hsize]
                    if digest != data[cd+hoff+n*hsize:cd+hoff+(n+1)*hsize]:
                        bad.append(n)
                signature = {'slot': slot, 'ad_hoc': bool(flags & 2), 'hash': algo, 'pages': slots, 'invalid_pages': bad,
                             'identifier': data[cd+ident:].split(b'\0')[0].decode(), 'special_slots': special}
                entry['signatures'].append(signature)
                if bad:
                    errors.append(f'{current}: {arch}: invalid signature pages {bad[:8]}')
        pos += size
    return entry

for p in sorted(bundle.rglob('*')):
    if not p.is_file() or p.is_symlink():
        continue
    with p.open('rb') as stream:
        magic = stream.read(4)
    if magic not in (b'\xcf\xfa\xed\xfe', b'\xca\xfe\xba\xbe', b'\xca\xfe\xba\xbf'):
        continue
    current = str(p.relative_to(bundle))
    data = p.read_bytes()
    slices = []
    if magic == b'\xcf\xfa\xed\xfe':
        slices = [data]
    else:
        count = struct.unpack_from('>I', data, 4)[0]
        for i in range(count):
            if magic[-1] == 0xbe:
                _, _, off, length, _ = struct.unpack_from('>5I',data,8+i*20)
            else:
                _, _, off, length, _, _ = struct.unpack_from('>IIQQII',data,8+i*32)
            slices.append(data[off:off+length])
    records = []
    for part in slices:
        cpu = struct.unpack_from('<I', part, 4)[0]
        records.append(inspect_slice(part, {0x1000007:'x86_64',0x100000c:'arm64'}.get(cpu,hex(cpu))))
    result['binaries'].append({'path': current, 'size': len(data), 'sha256':hashlib.sha256(data).hexdigest(),
                               'file':subprocess.check_output(['file','-b',str(p)],text=True).strip(), 'slices':records})

manifest_path = bundle / 'Frameworks/App.framework/flutter_assets/NativeAssetsManifest.json'
if manifest_path.is_file():
    manifest = json.loads(manifest_path.read_text())
    result['native_assets'] = manifest
    executable = next(b for b in result['binaries'] if b['path'] == info['CFBundleExecutable'])
    for part in executable['slices']:
        arch = {'x86_64': 'x64'}.get(part['architecture'], part['architecture'])
        for name, lookup in manifest.get('native-assets', {}).get('ios_' + arch, {}).items():
            if lookup[0] in ('absolute', 'relative'):
                target = bundle / 'Frameworks' / lookup[1]
                if not target.is_file():
                    errors.append(f'Missing native asset {name}: {lookup[1]}')
for binary in result['binaries']:
    for part in binary['slices']:
        for dependency in part['dependencies']:
            if dependency.startswith('@rpath/') and not (bundle / 'Frameworks' / dependency[7:]).is_file():
                errors.append(f"{binary['path']}: missing dependency {dependency}")
print(json.dumps(result, indent=2, default=str))
sys.exit(bool(errors))
