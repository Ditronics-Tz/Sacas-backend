# CRUSH.md

## Purpose
This file provides guidance for agentic coding agents like Crush to assist with software engineering tasks in this Go boilerplate repository.

## Build, Lint, and Test Commands
- **Build**: `go build ./cmd/...` - Builds the main application in the cmd directory.
- **Lint**: `golangci-lint run` - Runs linting checks on the codebase (ensure golangci-lint is installed).
- **Test All**: `go test ./...` - Runs all tests in the repository.
- **Single Test**: `go test -run TestName ./path/to/test` - Runs a specific test by name in the given directory.

## Code Style Guidelines
- **Imports**: Group imports into standard library, third-party, and internal packages, separated by blank lines.
- **Formatting**: Use `gofmt` for consistent formatting; no tabs, 2-space indentation.
- **Types**: Use explicit types for clarity; avoid type inference unless obvious (e.g., `var x int` over `x := 0`).
- **Naming Conventions**: Use camelCase for variables/functions, PascalCase for types, and avoid abbreviations unless widely understood.
- **Error Handling**: Always check and handle errors explicitly with `if err != nil`; log or return as appropriate.
- **File Structure**: Follow a clear directory structure (e.g., `cmd/`, `pkg/`, `internal/`) for modularity.

## Additional Notes
- Ensure all code adheres to Go best practices as outlined in Effective Go.
- No Cursor or Copilot rules found in the repository to include at this time.
- Agents should verify commands or style preferences with the user if unsure.