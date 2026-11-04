import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:test/test.dart';
import 'package:xcross/src/composition/simulator/simctl_simulator_launcher.dart';
import 'package:xcross/src/shared/errors/errors.dart';
import 'package:xcross/src/shared/models/pack_result.dart';

import '../cli/runtime_fixture.dart';

void main() {
  test(
    'SSH shared workspace installs without rsync and reports launch exit',
    () async {
      final temp = Directory.systemTemp.createTempSync('simulator-transport-');
      addTearDown(() => temp.deleteSync(recursive: true));
      final trace = p.join(temp.path, 'trace');
      final ssh = File(p.join(temp.path, 'ssh'))
        ..writeAsStringSync(
          '''#!/bin/sh\n'''
          r'''

printf '%s\n' "$*" >> "$SIM_TEST_TRACE"
case "$*" in
  *"'launch'"*) echo 'launch failed before VM startup' >&2; exit 42 ;;
esac
''',
        );
      await Process.run('chmod', ['+x', ssh.path]);
      final runtime = testRuntime(
        environment: {
          ...Platform.environment,
          'PATH': '${temp.path}:${Platform.environment['PATH']}',
          'SIM_TEST_TRACE': trace,
          'XCROSS_SIMULATOR_HOST': 'test-mac',
          'XCROSS_SIMULATOR_SHARED_ROOT': temp.path,
          'XCROSS_SIMULATOR_GUEST_ROOT': '/Users/Shared/Workspace',
        },
      );
      await expectLater(
        SimctlSimulatorLauncher(runtime).run(
          pack: PackResult(
            outputPath: p.join(temp.path, 'an app.app'),
            bundleId: 'test.app',
          ),
          hotReload: null,
        ),
        throwsA(
          isA<XcrossError>().having(
            (e) => e.toString(),
            'message',
            contains('42'),
          ),
        ),
      );
      final commands = File(trace).readAsStringSync();
      expect(commands, contains("'bootstatus' 'booted' '-b'"));
      expect(commands, contains("'/Users/Shared/Workspace/an app.app'"));
      expect(commands, contains("'codesign' '--force' '--deep' '--sign' '-'"));
      expect(commands, contains("'install' 'booted'"));
      expect(commands, contains("'terminate' 'booted' 'test.app'"));
      expect(commands, isNot(contains('rsync')));
    },
    testOn: 'linux',
  );
}
