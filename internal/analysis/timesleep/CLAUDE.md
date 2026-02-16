Analysis pass that forbids `time.Sleep` in `_test.go` files unless the call is lexically inside a `testing/synctest` bubble (the function literal passed to `synctest.Test`).

The check is purely lexical — a `time.Sleep` in a helper function defined outside the `synctest.Test` callback will be flagged even if the helper is only ever called from inside a bubble. This is intentional: keeping sleep calls inside the bubble makes the fake-time dependency obvious at the call site.

Tests use `analysistest` with GOPATH-style testdata under `testdata/src/a/`.
