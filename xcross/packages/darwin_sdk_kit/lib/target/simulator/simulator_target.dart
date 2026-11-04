import 'package:cli_kit/shared/platform/platform_host.dart';
import 'package:darwin_sdk_kit/target/shared/ios_target.dart';
import 'package:darwin_sdk_kit/target/simulator/simulator_build_platform.dart';

final class SimulatorTarget<T extends PlatformHostInterface>
    implements SimulatorTargetInterface<T> {
  const SimulatorTarget(this.host, {this.architecture});
  final String? architecture;

  String get selectedArchitecture {
    final selected =
        architecture ??
        host.environment.values['XCROSS_SIMULATOR_ARCH'] ??
        (const {'x64': 'x86_64', 'x86_64': 'x86_64'}[host.architecture] ??
            'arm64');
    if (selected != 'arm64' && selected != 'x86_64') {
      throw ArgumentError.value(
        selected,
        'XCROSS_SIMULATOR_ARCH',
        'Use x86_64 or arm64',
      );
    }
    return selected;
  }

  @override
  final T host;
  @override
  SimulatorBuildPlatform get buildPlatform =>
      SimulatorBuildPlatform(architecture: selectedArchitecture);
}
