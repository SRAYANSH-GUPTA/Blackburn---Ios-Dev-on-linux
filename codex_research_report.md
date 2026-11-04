Created the [deep technical report](</home/srayansh/Documents/Golang/Blackburn/reports/iOS compilation and simulator architectures.md>), with pinned source citations, command templates, and an engineering roadmap.

Key findings:

- xcross builds **arm64 Simulator bundles on Intel Linux**; macOS CoreSimulator executes them.
- SDK adaptation is targeted, with no broad rewriting of modulemaps or Swift interfaces.
- Your MacBook Air is the practical execution helper. Fully Linux-controlled hot reload needs a new remote-simulator adapter.
- Signing already exists; `gen_snapshot` is open source. General `actool`, `ibtool`, and Metal compilation remain gaps.
- Local virtualization, Darling, and `vphone-linux` have distinct limitations documented in the report.

This was a source audit; no SDK-backed build or simulator session was executed.