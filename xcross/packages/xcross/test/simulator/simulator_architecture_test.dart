import 'package:cli_kit/host/linux/linux_host.dart';
import 'package:darwin_sdk_kit/target/simulator/simulator_build_platform.dart';
import 'package:darwin_sdk_kit/target/simulator/simulator_target.dart';
import 'package:test/test.dart';
import 'package:xcross/src/composition/cli/flutter_build_command.dart';
import 'package:xcross/src/composition/ios_target.dart';
import 'package:xcross/src/composition/simulator/simctl_simulator_launcher.dart';
import 'package:xcross/src/shared/flutter/build/ios_deployment_target.dart';
import 'package:xcross/src/shared/flutter/build/runner_shim.dart';
import 'package:xcross/src/target/simulator/flutter/simulator_flutter_target.dart';

import '../cli/runtime_fixture.dart';

void main() {
  test('CLI arch overrides env through composed simulator build features', () {
    final runtime = testRuntime(
      environment: {'XCROSS_SIMULATOR_ARCH': 'x86_64'},
    );
    final command = FlutterBuildCommand(runtime);
    final parser = command.argParser;
    for (final arch in ['arm64', 'x86_64']) {
      final options = command.parseOptions(
        parser.parse(['--target-platform', 'simulator', '--arch', arch]),
      );
      final features = composeBuildFeatures(
        options.targetPlatform,
        runtime,
        architecture: options.arch,
      );
      expect(
        features.target.buildPlatform.swiftSdkTriple,
        '$arch-apple-ios-simulator',
      );
    }
    expect(command.parseOptions(parser.parse([])).arch, isNull);
    expect(() => parser.parse(['--arch', 'sparc']), throwsA(anything));
  });

  test('simulator host defaults to macbox on Linux and honors overrides', () {
    expect(SimctlSimulatorLauncher(testRuntime()).host, 'macbox');
    expect(
      SimctlSimulatorLauncher(
        testRuntime(environment: {'XCROSS_SIMULATOR_HOST': 'my-air'}),
      ).host,
      'my-air',
    );
    expect(
      SimctlSimulatorLauncher(
        testRuntime(environment: {'XCROSS_SIMULATOR_HOST': ''}),
      ).host,
      '',
    );
  });

  test(
    'Intel Linux defaults to x86_64; env and explicit architecture override',
    () {
      for (final arch in ['x64', 'x86_64']) {
        expect(
          SimulatorTarget(LinuxHost(architecture: arch)).selectedArchitecture,
          'x86_64',
        );
      }
      expect(
        SimulatorTarget(LinuxHost(architecture: 'arm64')).selectedArchitecture,
        'arm64',
      );
      final host = LinuxHost(architecture: 'x64');
      expect(
        SimulatorTarget(host).buildPlatform.swiftSdkTriple,
        'x86_64-apple-ios-simulator',
      );
      final armHost = LinuxHost(
        architecture: 'x64',
        environment: {'XCROSS_SIMULATOR_ARCH': 'arm64'},
      );
      expect(
        SimulatorTarget(armHost).buildPlatform.swiftSdkTriple,
        'arm64-apple-ios-simulator',
      );
      expect(
        SimulatorTarget(
          armHost,
          architecture: 'x86_64',
        ).buildPlatform.buildTriple('15.0'),
        'x86_64-apple-ios15.0-simulator',
      );
      final bad = LinuxHost(environment: {'XCROSS_SIMULATOR_ARCH': 'sparc'});
      expect(() => SimulatorTarget(bad).buildPlatform, throwsArgumentError);
    },
  );

  for (final arch in ['arm64', 'x86_64']) {
    test('$arch selects correct linker platform, architecture and runtime', () {
      final deployment = IosDeploymentTarget(
        '15.0',
        platform: SimulatorBuildPlatform(architecture: arch),
      );
      final args = RunnerShim.linkArguments(
        objectPath: 'Runner.o',
        outputPath: 'Runner',
        iosSdk: '/sdk',
        flutterSlice: '/Flutter',
        subframeworks: '/sub',
        sdkVersion: '26.0',
        deploymentTarget: deployment,
        compilerRuntime: '/rt/libclang_rt.iossim.a',
      );
      expect(args[args.indexOf('-arch') + 1], arch);
      expect(args, contains('ios-simulator'));
      expect(args, contains('/rt/libclang_rt.iossim.a'));
      final policy = SimulatorFlutterTarget(
        SimulatorTarget(LinuxHost(), architecture: arch),
      );
      expect(
        policy.engineSliceIdentifiers,
        contains('ios-${arch == 'arm64' ? 'arm64' : 'x86_64'}-simulator'),
      );
      expect(
        policy.engineSliceIdentifiers,
        isNot(
          contains('ios-${arch == 'arm64' ? 'x86_64' : 'arm64'}-simulator'),
        ),
      );
    });
  }

  test(
    'VM discovery preserves authentication path through websocket conversion',
    () {
      final uri = SimctlSimulatorLauncher.parseVmService(
        'flutter: The Dart VM service is listening on http://127.0.0.1:54321/aBc=/',
      );
      expect(uri, isNotNull);
      expect(
        SimctlSimulatorLauncher.websocketUri(
          uri!.replace(port: 12345),
        ).toString(),
        'ws://127.0.0.1:12345/aBc=/ws',
      );
      expect(
        SimctlSimulatorLauncher.parseVmService(
          'unrelated http://127.0.0.1:1234/',
        ),
        isNull,
      );
      expect(
        SimctlSimulatorLauncher.parseVmService(
          'The Dart VM service is listening on http://192.0.2.1:1234/',
        ),
        isNull,
      );
    },
  );

  test('remote shell quoting preserves spaces and shell metacharacters', () {
    expect(
      SimctlSimulatorLauncher.quote(r"/a path/it's $HOME; hi"),
      r"'/a path/it'\''s $HOME; hi'",
    );
  });
}
