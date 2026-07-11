# Contributing

Contributions are welcome, especially synthetic fixtures, documentation corrections, cross-platform codec work, and verified support for additional device revisions.

## Before opening a pull request

1. Open an issue for changes that alter the wire protocol or hardware behavior.
2. Do not attach or commit vendor binaries, installers, PPDs, disassembly, private captures, or copied/decompiled source.
3. Add synthetic tests for behavior changes.
4. Run:

```bash
swift run F11CoreTests
swift build -c release
```

5. Keep USB tests opt-in. CI and ordinary tests must never require a printer.

## Protocol evidence

Clearly label evidence as one of:

- statically documented;
- synthetic-test confirmed;
- official-output observed;
- physically device-confirmed; or
- inferred/unresolved.

Do not generalize F11 behavior to another model without captures and controlled validation.

## Style

- Prefer explicit errors over force unwraps.
- Keep protocol generation independent from transport.
- Validate generated streams with an independently structured decoder.
- Preserve deterministic output where practical.
- Document byte order and field width for every new wire field.

By contributing, you agree that your original contribution is licensed under the repository's MIT License and that you have the right to submit it.
