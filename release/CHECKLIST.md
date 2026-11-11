# Release checklist

Verified against `.github/workflows/release.yml` and the `Makefile`.

1. `make lint && make test-race` pass on `main`.
2. Re-run `benchmarks/perf.sh` and commit any changed results. (This script does not exist yet; until it does, run `benchmarks/benchmark.sh`.)
3. Re-render the demo (`vhs docs/demo/llm-filesystem.tape`) if a command it shows has changed.
4. Cut the changelog and update any affected `docs/` page.
5. Optional dry run: `make build-all` builds the five binaries into `dist/` locally.
6. Push a `v*` tag, for example `git tag v1.0.3 && git push origin v1.0.3`.
7. The `release` workflow then verifies (gofmt, vet, `make build`, `go test -race`), builds five binaries (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64) with the tag stamped as the version, writes `SHA256SUMS.txt`, checks five binaries and five matching checksums, and publishes the release.
8. Check the published release has the five binaries and `SHA256SUMS.txt`.
