# Third-Party Notices

conoha-proxy is licensed under Apache-2.0. It uses the following third-party
Go modules, each retaining its original license.

## Dependencies

| Module | License | URL |
|---|---|---|
| github.com/caddyserver/certmagic | Apache-2.0 | https://github.com/caddyserver/certmagic |
| go.etcd.io/bbolt | MIT | https://github.com/etcd-io/bbolt |
| github.com/spf13/cobra | Apache-2.0 | https://github.com/spf13/cobra |
| github.com/stretchr/testify | MIT | https://github.com/stretchr/testify |

(Transitive dependencies, including `libdns/libdns`, `mholt/acmez`, `miekg/dns`, `caddyserver/zerossl`, `zeebo/blake3`, `klauspost/cpuid`, `spf13/pflag`, `inconshreveable/mousetrap`, `go.uber.org/zap`, `go.uber.org/multierr`, and the `golang.org/x/*` packages, are pulled in automatically. All are permissive licenses — see their repositories for the full text.)

## Test-only

| Module | License | URL |
|---|---|---|
| github.com/letsencrypt/pebble/v2 | MPL-2.0 | https://github.com/letsencrypt/pebble |

Pebble is launched as a separate process in e2e tests only; it is NOT bundled in the shipped binary or Docker image.
