This is a custom `go vet` tool that bundles all standard analyzers with project-specific ones. Run it with:

```
go vet -vettool=$(go build -print ./cmd/vet) ./...
```

When adding a new analyzer, import it and append it to the `multichecker.Main` call in `main.go`.
