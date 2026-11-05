"""Download package ranges into reims-util's reserved image region; it verifies on resume."""
import concurrent.futures
import json
import os
from pathlib import Path
import time
import urllib.request

root = Path('/home/srayansh/Documents/Golang/Blackburn/verification/choice-b')
info = json.loads((root / 'selected-installer.json').read_text())
image = Path('/home/srayansh/.local/share/macbox/offline/macOS-Tahoe-26.7.2-25G332-offline-installer.img.part')
offset = 3571712
fd = os.open(image, os.O_RDWR)
assert os.pread(fd, 4, offset) == b'xar!'
size = info['size']
block = 32 * 1024 * 1024

def fetch(start):
    end = min(start + block, size) - 1
    for attempt in range(8):
        try:
            request = urllib.request.Request(info['url'], headers={'Range': f'bytes={start}-{end}', 'Accept-Encoding': 'identity'})
            with urllib.request.urlopen(request, timeout=45) as response:
                expected = f'bytes {start}-{end}/{size}'
                if response.status != 206 or response.headers.get('Content-Range') != expected:
                    raise ValueError(f'Invalid range response: {response.status}, {response.headers.get("Content-Range")}')
                pos = start
                while pos <= end:
                    data = response.read(min(1024 * 1024, end - pos + 1))
                    if not data:
                        raise EOFError(f'Range ended at {pos}, expected {end}')
                    written = os.pwrite(fd, data, offset + pos)
                    if written != len(data):
                        raise OSError('Short disk write')
                    pos += written
                return end - start + 1
        except Exception as error:
            print(json.dumps({'retry': start, 'attempt': attempt + 1, 'error': str(error)}), flush=True)
            if attempt == 7:
                raise
            time.sleep(min(2 ** attempt, 10))

try:
    # reims-util already checked this prefix against Apple's chunklist.
    prefix = 384 * 1024 * 1024
    completed = prefix
    with concurrent.futures.ThreadPoolExecutor(max_workers=22) as pool:
        jobs = [pool.submit(fetch, start) for start in range(prefix, size, block)]
        for job in concurrent.futures.as_completed(jobs):
            completed += job.result()
            print(json.dumps({'done': completed, 'total': size, 'percent': round(completed * 100 / size, 2)}), flush=True)
    os.fsync(fd)
finally:
    os.close(fd)
