import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:cli_kit/shared/platform/platform_host.dart';
import 'package:dart_mobile_device/shared/device/models/device_endpoint.dart';
import 'package:frontend_server_kit/host/shared/process/host_compiler_process_factory.dart';
import 'package:meta/meta.dart';
import 'package:xcross/src/shared/errors/errors.dart';
import 'package:xcross/src/shared/flutter/hot_reload/dart_vm_service_client.dart';
import 'package:xcross/src/shared/flutter/hot_reload/hot_reload_controller.dart';
import 'package:xcross/src/shared/flutter/models/hot_reload_config.dart';
import 'package:xcross/src/shared/models/pack_result.dart';
import 'package:xcross/src/shared/runtime/xcross_runtime.dart';

/// Runs CoreSimulator locally on macOS or over an SSH alias (including macbox).
/// SSH configuration owns credentials, ports and host-key verification.
@internal
final class SimctlSimulatorLauncher<T extends PlatformHostInterface> {
  SimctlSimulatorLauncher(this.runtime);
  final XcrossRuntime<T> runtime;
  Map<String, String> get _env => runtime.runner.effectiveEnvironment;
  String? get _host => _env['XCROSS_SIMULATOR_HOST'];

  static String quote(String value) => "'${value.replaceAll("'", r"'\''")}'";

  static Uri? parseVmService(String line) {
    final match = RegExp(
      r'The Dart VM service is listening on (http://(?:127\.0\.0\.1|localhost|\[::1\]):\d+/\S*)',
    ).firstMatch(line);
    return match == null ? null : Uri.tryParse(match.group(1)!);
  }

  static Uri websocketUri(Uri http) => http.replace(
    scheme: 'ws',
    path: '${http.path.endsWith('/') ? http.path : '${http.path}/'}ws',
  );

  Future<Process> _start(List<String> args) {
    final host = _host;
    return host == null || host.isEmpty
        ? runtime.host.processes.start(
            args.first,
            args.skip(1).toList(),
            environment: _env,
          )
        : runtime.host.processes.start('ssh', [
            '-o',
            'BatchMode=yes',
            '-o',
            'ConnectTimeout=10',
            '--',
            host,
            args.map(quote).join(' '),
          ], environment: _env);
  }

  Future<void> _checked(List<String> args) async {
    final process = await _start(args);
    final output = process.stdout.transform(utf8.decoder).join();
    final errors = process.stderr.transform(utf8.decoder).join();
    final code = await process.exitCode;
    final text = '${await output}${await errors}';
    if (code != 0) throw XcrossError('${args.first} failed ($code): $text');
  }

  Future<String> _stage(String app) async {
    if (_host == null || _host!.isEmpty) return app;
    final paths = runtime.host.paths.context;
    final shared = _env['XCROSS_SIMULATOR_SHARED_ROOT'];
    final guest = _env['XCROSS_SIMULATOR_GUEST_ROOT'];
    if (shared != null && guest != null && paths.isWithin(shared, app)) {
      return '$guest/${paths.relative(app, from: shared).replaceAll(r'\', '/')}';
    }
    // Unique staging directory; rsync keeps symlinks and executable modes.
    final remote =
        '/tmp/xcross-simulator-$pid-${DateTime.now().microsecondsSinceEpoch}';
    _staging = remote;
    await _checked(['mkdir', '-p', remote]);
    final sync = await runtime.host.processes.start('rsync', [
      '-a',
      '--protect-args',
      '-e',
      'ssh -o BatchMode=yes',
      '--',
      app,
      '${_host!}:$remote/',
    ], environment: _env);
    final out = sync.stdout.drain<void>();
    final err = sync.stderr.transform(utf8.decoder).join();
    final code = await sync.exitCode;
    await out;
    final errors = await err;
    if (code != 0) throw XcrossError('Simulator app sync failed: $errors');
    return '$remote/${paths.basename(app)}';
  }

  Future<void> _cleanup(List<String> args) async {
    try {
      await _checked(args).timeout(const Duration(seconds: 10));
    } on Object catch (error) {
      runtime.log.logTrace('Simulator cleanup: $error');
    }
  }

  String? _staging;

  Future<void> run({
    required PackResult pack,
    required HotReloadConfig? hotReload,
    String device = 'booted',
    List<String> arguments = const [],
  }) async {
    if ((_host == null || _host!.isEmpty) && runtime.host.name != 'macos') {
      throw XcrossError(
        'Set XCROSS_SIMULATOR_HOST to an SSH Mac alias (e.g. macbox).',
      );
    }
    Process? launch;
    Process? tunnel;
    HotReloadController? controller;
    DartVmServiceClient? vm;
    final subscriptions = <StreamSubscription<dynamic>>[];
    final stopped = Completer<void>();
    void stop() {
      if (!stopped.isCompleted) stopped.complete();
    }

    var terminalChanged = false;
    final previousEcho =
        !runtime.outputHasTerminal || !stdin.hasTerminal || stdin.echoMode;
    final previousLine =
        !runtime.outputHasTerminal || !stdin.hasTerminal || stdin.lineMode;
    try {
      if (device != 'booted') {
        // Boot may report already booted. bootstatus below is authoritative.
        final boot = await _start(['xcrun', 'simctl', 'boot', device]);
        await Future.wait([
          boot.stdout.drain<void>(),
          boot.stderr.drain<void>(),
        ]);
        await boot.exitCode;
      }
      await _checked(['xcrun', 'simctl', 'bootstatus', device, '-b']);
      final app = await _stage(pack.appPath);
      await _checked(['codesign', '--force', '--deep', '--sign', '-', app]);
      await _checked(['xcrun', 'simctl', 'install', device, app]);
      final ready = Completer<Uri>();
      launch = await _start([
        'xcrun',
        'simctl',
        'launch',
        '--terminate-running-process',
        '--console-pty',
        device,
        pack.bundleId,
        '--enable-dart-profiling',
        '--enable-checked-mode',
        '--vm-service-host=127.0.0.1',
        '--vm-service-port=0',
        ...arguments,
      ]);
      void line(String line) {
        runtime.output.writeln(line);
        final uri = parseVmService(line);
        if (uri != null && !ready.isCompleted) ready.complete(uri);
      }

      for (final stream in [launch.stdout, launch.stderr]) {
        subscriptions.add(
          stream
              .transform(utf8.decoder)
              .transform(const LineSplitter())
              .listen(line),
        );
      }
      unawaited(
        launch.exitCode.then((code) {
          if (!ready.isCompleted) {
            ready.completeError(
              XcrossError(
                'Simulator launch exited ($code) before VM service discovery.',
              ),
            );
          }
          stop();
        }),
      );
      subscriptions.add(ProcessSignal.sigint.watch().listen((_) => stop()));
      subscriptions.add(ProcessSignal.sigterm.watch().listen((_) => stop()));
      var uri = await Future.any([
        ready.future,
        stopped.future.then<Uri>(
          (_) => throw XcrossError('Simulator session stopped.'),
        ),
      ]).timeout(const Duration(seconds: 90));
      if (_host != null && _host!.isNotEmpty) {
        final socket = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
        final port = socket.port;
        await socket.close();
        tunnel = await runtime.host.processes.start('ssh', [
          '-N',
          '-o',
          'BatchMode=yes',
          '-o',
          'ExitOnForwardFailure=yes',
          '-o',
          'ServerAliveInterval=15',
          '-o',
          'ServerAliveCountMax=3',
          '-L',
          '127.0.0.1:$port:127.0.0.1:${uri.port}',
          '--',
          _host!,
        ], environment: _env);
        subscriptions.add(tunnel.stdout.listen((_) {}));
        subscriptions.add(
          tunnel.stderr.transform(utf8.decoder).listen(runtime.errors.write),
        );
        unawaited(tunnel.exitCode.then((_) => stop()));
        uri = uri.replace(host: '127.0.0.1', port: port);
      }
      // SSH fork/forward readiness is asynchronous. Retry bounded connections.
      final deadline = DateTime.now().add(const Duration(seconds: 20));
      while (true) {
        vm = DartVmServiceClient(
          log: runtime.log,
          connector: runtime.vmConnector,
        );
        try {
          await vm.connect(
            websocketUri(uri),
            timeout: const Duration(seconds: 1),
          );
          break;
        } on Object {
          await vm.close();
          if (stopped.isCompleted || DateTime.now().isAfter(deadline)) rethrow;
          await Future<void>.delayed(const Duration(milliseconds: 200));
        }
      }
      // Publish only the reachable host-side URI, retaining the auth path.
      // XcrossDap watches this exact marker to connect its debugger.
      runtime.output.writeln('vm-service: ${websocketUri(uri)}');
      vm.onConnectionLost = stop;
      if (hotReload != null) {
        controller = HotReloadController(
          config: hotReload,
          log: runtime.log,
          localHttp: runtime.localHttp,
          vm: vm,
          vmService: DeviceEndpoint(host: uri.host, port: uri.port),
          vmServiceUri: uri,
          processFactory: HostCompilerProcessFactory(runtime.runner),
          diagnostics: runtime.log.logTrace,
        );
        await controller.initialSync();
        runtime.log.logInfo('Hot reload ready: r reload, R restart, q quit.');
      }
      if (runtime.outputHasTerminal && stdin.hasTerminal) {
        stdin.echoMode = false;
        stdin.lineMode = false;
        terminalChanged = true;
      }
      // Serialize reloads; a second key cannot race a frontend compilation.
      var pending = Future<void>.value();
      subscriptions.add(
        runtime.input.listen((bytes) {
          for (final byte in bytes) {
            if (byte == 113 || byte == 3) {
              stop();
              continue;
            }
            pending = pending
                .then((_) async {
                  if (stopped.isCompleted) return;
                  if (byte == 114) await controller?.reload();
                  if (byte == 82) await controller?.restart();
                })
                .catchError((Object error) {
                  runtime.log.logWarn('Reload failed: $error');
                });
          }
        }),
      );
      await stopped.future;
      await pending;
    } finally {
      if (terminalChanged) {
        stdin.echoMode = previousEcho;
        stdin.lineMode = previousLine;
      }
      for (final subscription in subscriptions) {
        await subscription.cancel();
      }
      await controller?.close();
      await vm?.close();
      launch?.kill();
      tunnel?.kill();
      // simctl's console disconnect need not terminate the app.
      if (launch != null) {
        await _cleanup(['xcrun', 'simctl', 'terminate', device, pack.bundleId]);
      }
      final staging = _staging;
      if (staging != null) await _cleanup(['rm', '-rf', staging]);
    }
  }
}
