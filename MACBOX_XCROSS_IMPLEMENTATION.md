Implemented on `feat/macbox-xcross-simulator`: x86_64/ARM64 Simulator builds, SSH `simctl` execution, VM-service forwarding, hot reload, macbox profile and helper CLI.

Validation: analyzer clean; 100 SDK tests and 980 Flutter/CLI tests passed, with 9 skips. Host doctor passed. Actual macOS/Simulator execution, SMB sharing and live hot reload still require guest provisioning and hardware acceptance testing.

Detailed configuration, architecture notes, prerequisites and troubleshooting: [MACBOX_XCROSS_GUIDE.md](docs/MACBOX_XCROSS_GUIDE.md).

Run these commands in Bash:

```bash
export BLACKBURN=/home/srayansh/Documents/Golang/Blackburn
cd "$BLACKBURN"
git submodule update --init --recursive
(cd xcross && dart pub get)
./scripts/blackburn-macbox.sh doctor
./scripts/blackburn-macbox.sh setup-macbox
```

Complete macOS installation/account setup using `macbox next` and [the setup guide](macbox/docs/setup.md). Then:

```bash
export MACBOX_CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/macbox/blackburn.env"
source "$BLACKBURN/macbox/.env.blackburn"
export MACBOX_WORKSPACE="$BLACKBURN"
"$BLACKBURN/macbox/macbox" ssh-bootstrap
"$BLACKBURN/macbox/macbox" provision
"$BLACKBURN/macbox/macbox" xcode /path/to/Xcode.xip
"$BLACKBURN/macbox/macbox" pool
"$BLACKBURN/scripts/blackburn-macbox.sh" extract-sdk /path/to/Xcode.xip
```

From your Flutter project:

```bash
cd /path/to/flutter-app
"$BLACKBURN/scripts/blackburn-macbox.sh" build-sim
"$BLACKBURN/scripts/blackburn-macbox.sh" run-sim
# r = reload; R = restart; q = quit
```

The default target is x86_64. For a remote Apple Silicon Mac, set `XCROSS_SIMULATOR_ARCH=arm64` and `XCROSS_SIMULATOR_HOST=your-ssh-alias`, then pass `--udid YOUR_SIMULATOR_UDID` to `run-sim`.

For bundles inside `$BLACKBURN`, run `scripts/blackburn-macbox.sh mount-workspace`, then use `BLACKBURN_SHARED=1` with `run-sim`. This mounts container `/workspace` in the guest at `/Users/Shared/BlackburnWorkspace`; other runs use rsync. See the detailed guide for existing-VM migration and license constraints.
