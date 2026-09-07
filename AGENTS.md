# Veer

- Keep Veer TUI-only; argument dispatch is reserved for the private elevation helper.
- Keep core details in `engine`, process lifecycle in `session`, OS integration in `network`/`privilege`, and terminal interaction in `tui`.
- Run blocking I/O outside Bubble Tea `Update`/`View`. Preserve cancellation, DNS rollback and exactly one `Wait` per child process.
- Sanitize terminal output and keep credentials out of views. Downloads and server-file writes require an explicit TUI action.
- Run `just fmt`, `just lint`, `just test` and `just build`. Cross-compile Windows/Linux when platform code changes; cross-compilation does not validate UAC or TUN behavior.
- Test observable behavior using subprocesses or fakes at OS boundaries. Tests must not change the host network.
