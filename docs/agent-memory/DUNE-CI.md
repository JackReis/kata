# Dune import gates

Pointer only. The contract lives in [`docs/DUNE-CI.md`](../DUNE-CI.md).

`make dune-import` is the local gate. Exit 0 is a clean production graph. Exit 1 is a banned edge. Exit 2 means the gate is missing or unsound: do not ship. Run the built binary; `go run` collapses those codes to 1.
