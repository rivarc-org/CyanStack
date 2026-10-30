# CyanStack (archived prototype)

> **Status: archived.** This was an exploratory prototype and is no longer maintained.
> Rivarc is being redesigned from scratch in [rivarc-org/rivarc](https://github.com/rivarc-org/rivarc).

CyanStack was a quick, AI-assisted prototype built to test one idea: deploying
self-hosted software by connecting services in a graph, with credentials generated
and passed along each connection instead of being configured by hand.

The visual editor lives in [cyan-ui](https://github.com/rivarc-org/cyan-ui).

## What it tried

- A Go service that deploys Docker-based services from manifests
  (see [`example-config.yaml`](example-config.yaml) for an early Medusa manifest)
- Manifests declaring inputs (ports, secrets, connections to other services),
  environment mappings, lifecycle hooks (install, migration, health check) and exports
- Secrets generated per deployment and injected into the services that need them

## What it showed

The interaction works: connecting two services is enough to configure both.
But a model that runs a demo is far from one that runs production software.

- **Reliability was the biggest problem:** Random bugs & issues caused frequent downtime.
- **The manifest model was too thin.** The Medusa manifest covers a database, a
  secret and a seed command. Production Medusa v2 also needs separate server and
  worker processes, Redis, migrations ordered before startup, and a revocable
  publishable key for each connected storefront. Its migration command was also
  written for an older Medusa CLI, so lifecycle steps clearly depend on the app version.
- **Connections had no clear semantics.** Configuration, access and network
  reachability were mixed together, and nothing could be revoked cleanly.

These lessons shaped Rivarc: careful contracts first, then a reference implementation.

