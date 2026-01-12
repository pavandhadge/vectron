# How to Run End-to-End (E2E) Tests

This document provides instructions for running the end-to-end tests located in the main project directory.

## Prerequisites

1.  **Go:** Ensure you have a working Go environment.
2.  **Docker:** The tests use a Docker container for the `etcd` service. Make sure Docker is installed and running.
3.  **Make:** The tests rely on a `Makefile` to build the service binaries before execution.

## Running All E2E Tests

The e2e tests are standard Go tests, but they require a specific environment variable to be set for handling JWT secrets.

To run all `e2e_*_test.go` files in the root directory, execute the following command:

```bash
e2eJwtSecret="a-super-secret-key" go test -v -run E2E ./...
```

### Command Breakdown:

-   `e2eJwtSecret="a-super-secret-key"`: This sets the required environment variable for the test run. The specific value doesn't matter for the test environment.
-   `go test`: The standard Go test command.
-   `-v`: Enables verbose output, which is helpful for seeing the test progress and logs from the services being started.
-   `-run E2E`: This is a regular expression that matches test functions to run. By convention, all E2E tests are named with the prefix `TestE2E_`. This pattern ensures only these tests are executed.
-   `./...`: This tells the Go tool to run tests in the current directory and all subdirectories.

## Running a Specific E2E Test

If you want to run a single test function, you can make the `-run` pattern more specific. For example, to run only the `TestE2E_NewRoutes` test:

```bash
e2eJwtSecret="a-super-secret-key" go test -v -run TestE2E_NewRoutes ./...
```

## How It Works

When you run these tests, the `TestMain` function in `e2e_test.go` is executed first. It performs the following setup:

1.  Runs `make build` to compile all the necessary service binaries (`placementdriver`, `worker`, `auth`, `apigateway`).
2.  The tests themselves then start these binaries as background processes, along with an embedded `etcd` instance.
3.  The test logic executes against these running services.
4.  After the tests complete, all started services are automatically shut down.
