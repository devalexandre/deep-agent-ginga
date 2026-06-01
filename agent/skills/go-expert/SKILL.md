---
name: go-expert
description: "Expert skills for Golang development, including testing, dependency management, and code quality."
metadata:
  version: "1.0.0"
  author: agno-team
---

# Go Expert Skill

Use this skill when working on Go (Golang) projects to ensure idiomatic code, proper testing, and efficient dependency management.

## Guidelines for Go Development

1. **Idiomatic Go**: Use modern Go idioms (Go 1.25+).
   - Prefer `any` over `interface{}`.
   - Use `errors.Is` and `errors.As` for error handling.
   - Use `slices` and `maps` standard library packages for common operations.
   - Use `for i := range n` for simple loops.
   - Use `t.Context()` in tests.

2. **Testing**:
   - Always check for `_test.go` files when modifying a package.
   - Run tests using `go test ./...` or `go test -v ./path/to/package`.
   - Use `stretchr/testify` if already present in the project.

3. **Dependencies**:
   - Use `go mod tidy` after adding or removing imports.
   - Check `go.mod` to understand the project structure and dependencies.

4. **Code Quality**:
   - Use `go fmt ./...` to format code.
   - Use `go vet ./...` to check for common errors.

## Useful Commands

- `go mod tidy`: Clean up dependencies.
- `go test -v ./...`: Run all tests with verbose output.
- `go build ./...`: Ensure everything compiles.
- `go list -m all`: List all dependencies.
