// Package importban is the FAIL-closed import-graph gate for Kata.
//
// Browser and shared UI sources may not reach host internals, secret env
// readers, or a second assignment client. Public packages under pkg/ may not
// import privileged daemon internals except the declared allowlist. The check
// reads files through the os package so go test records them as cache inputs.
package importban
