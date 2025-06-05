# Go Project Guidelines

## General Best Practices
- Write code that is easy for humans to understand and maintain.
- **Follow idiomatic Go practices**: This means writing code that aligns with Go's core design philosophy and the common conventions established by its community. The aim is to produce code that is straightforward, clear, efficient, and easily understood by other Go developers. It involves:
    - Adhering to the language's intended style and structure.
    - Applying patterns and guidelines widely accepted and often exemplified in the official Go documentation and standard library.
    - Focusing on simplicity and readability to ensure the code "looks like Go".
- Strive for code clarity and minimize technical debt.
- Organize source code logically at the package level.
- Ensure standardization in formatting, linting, and testing approaches.

## Code Style & Patterns
- Use `golangci-lint` for all linting and formatting.
    - Linting flags: `--path-mode=abs --fast-only`
    - Formatting is done via `golangci-lint fmt --stdin`
- For HTML templating, use standard Go templating libraries and practices.
    - Organize templates into "blocks" and "components".
- Prefer composition over inheritance where applicable.

## Testing Standards
- Write tests at every level of the application: unit tests and integration tests.
- Use the `testify` library for assertions in unit tests.
- Keep unit tests simple and focused.
- Test cases, especially in table-driven tests, should be run in their own subtests for clear output.
- Ensure test error messages are helpful and provide enough context, including `got` and `want` values.
- Use Go's standard `testing` package for the overall test structure, complemented by `testify` for assertions.

## Build & Development Process
- Use `mage` for tasks such as linting, serving, and building the application.
    - Common targets: `mage lint`, `mage serve path/to/app`
    - Build flags: `-tags=mage`
- Make `golangci-lint` a required part of your development and automated build process.

## Error Handling
- Use `errors.New` for creating new error messages.
- Use `slog.Error` to log before returning the error

## Logging Conventions with slog
- Standard logging levels:
    - `INFO`: For general operational information.
    - `WARN`: For serious issues that do not cause a crash but might indicate a problem.
    - `ERROR`: For errors that prevent the application from continuing its current operation, potentially leading to a panic if unrecoverable.
- Use `panic` for unrecoverable errors where the application cannot safely continue.

## Package Structure
- Aim for a Clean Architecture approach, but this is an evolving guideline.

## API Design (if applicable)
- (To be defined - no specific conventions established yet)

## Concurrency Patterns
- (To be defined - no specific conventions established yet)

## Commit Message Conventions
- Follow Conventional Commits format for all commit messages.