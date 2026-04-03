# Anchor

A homelab-friendly configuration management system. One binary, plain shell
scripts, no YAML DSL.

## Why Anchor?

Tools like Ansible, Chef, and Puppet are powerful but carry significant
operational overhead: package dependencies, DSLs to learn, external databases,
complex agent runtimes. If you manage a handful of Linux machines and want
idempotent configuration with visibility into what's happening, Anchor is the
simpler alternative.

- **Single binary** -- server, agent, key management, and signing all in one. No runtime dependencies.
- **Modules are shell scripts** -- no DSL. If you can write a shell script, you can write a module.
- **Embedded SQLite** -- no external database to provision or maintain.
- **Web UI included** -- dark mode, agent status, module assignment. No build step, no CDN.
- **Optional module signing** -- ed25519 signatures with SSH key support, so
  you can verify modules came from a trusted source.

## How it works

The **server** reads modules from a directory, serves a web UI, and responds to
agent check-ins. The **agent** polls the server, downloads assigned modules and
their artifacts, runs them, and reports results back.

```
            ┌────────────┐
            │  modules/  │
            │            │
            │  00_base   │
            │  10_nginx  │
            │  20_certs  │
            └──────▲─────┘
              read │
            ┌──────┴─────┐
            │   Server   │
            │            │
            │  SQLite DB │
            │  Web UI    │
            └──────▲─────┘
                   │
      ┌────────────┼────────────────┐
      │            │                │
┌─────┴──────┐ ┌───┴───────┐ ┌──────┴─────┐
│  Agent 1   │ │  Agent 2  │ │  Agent 3   │
│  (web-1)   │ │  (db-1)   │ │  (nas-1)   │
└─────┬──────┘ └───┬───────┘ └──────┬─────┘
      │            │                │
      └────────────┼────────────────┘
                   │
           each agent loop:
           1. poll server for assigned modules
           2. fetch scripts + artifacts
           3. run each module, ordered lexicographically
           4. report result(s) to server (ok / changed / error)
```

## Quick start

### Build

Requires Go 1.26+. No cgo needed.

```sh
go build ./cmd/anchor
```

Or use the Makefile:

```sh
make build    # build for current platform
make ci       # full check suite (fmt, vet, build, test)
```

### Run the server

```sh
anchor server \
  -port 8080 \
  -modules-dir /etc/anchor-server/modules.d \
  -data-dir /var/lib/anchor-server
```

### Run an agent

```sh
anchor agent \
  -server http://your-server:8080 \
  -data-dir /var/lib/anchor
```

Send `SIGHUP` to trigger an immediate check-in cycle. systemd unit files are
provided in `contrib/`.

## Modules

A module is an executable script that accepts a single command argument:

```sh
#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "Hostname Check", "description": "Reports the current hostname"}'
        ;;
    apply)
        echo "hostname: $(hostname)"
        echo "fqdn: $(hostname -f 2>/dev/null || echo unknown)"
        exit 0   # 0 = ok, 80 = changed, anything else = error
        ;;
esac
```

The **filename** (e.g. `00_hostname`) is the canonical identifier -- modules
execute in filename-sorted order. See `example/modules.d/` for more examples.

### Artifacts

A module can have an associated `<filename>.d/` directory containing supporting
files (config templates, binaries, etc.). These are distributed to agents via
content-addressable caching with preserved file permissions. Scripts access
them as relative paths in their working directory.

### Signing

Signing lets agents verify that modules haven't been tampered with before
running them. It's entirely opt-in -- agents that don't specify any
`--verify-key` flags will run unsigned modules without complaint.

Generate a keypair and sign your modules on the server:

```sh
anchor keygen -o mykey
anchor sign -k mykey.key modules.d/00_hostname
```

Then tell agents to require valid signatures:

```sh
anchor agent -server http://server:8080 \
  --verify-key /etc/anchor/mykey.pub
```

When verification is enabled, agents reject modules with missing or invalid
signatures and report an error status to the server.

`--verify-key` is repeatable and accepts multiple key formats: Anchor's own
PEM files (produced by `anchor keygen`), OpenSSH public keys
(`~/.ssh/id_ed25519.pub`), or SSH public key files.

You can also use `--verify-key-url` to fetch keys from a URL in
`authorized_keys` format -- for example, `https://github.com/youruser.keys`.
URL-fetched keys are cached locally for offline fallback.

## Status

Anchor is in early, active development by a single author. The core feature set
(server, agent, web UI, artifacts, signing) is functional, but the project
should be considered pre-release software.

## License

See [LICENSE](LICENSE).
