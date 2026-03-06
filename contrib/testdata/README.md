# contrib/testdata

Test fixtures for the `contrib` package integration tests.

- **Containerfile** — Builds a Debian image with systemd and curl for running systemd unit tests in a container.
- **setup.sh** — Runs inside the container to install unit files, start services, and verify they are healthy.
- **00_test** — A minimal anchor module used to populate the server's modules directory during tests.
