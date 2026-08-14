# Baseline de supply chain

Versões adotadas e verificadas em 2026-08-14:

| Action oficial | Release | SHA completo |
| --- | --- | --- |
| `actions/checkout` | `v7.0.1` | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | `v7.0.0` | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| `actions/setup-node` | `v7.0.0` | `820762786026740c76f36085b0efc47a31fe5020` |

Essas releases oficiais declaram runtime Node 24. Os SHAs, e não tags flutuantes, são usados no workflow. A atualização exige nova verificação do repositório oficial, release e conteúdo de `action.yml`.

O CI fixa Go 1.26.6 e Node.js 24.19.0. npm 11.17.0 é registrado em `packageManager`; dependências npm, incluindo Redocly CLI 2.46.1 para OpenAPI 3.1, estão fixadas no lockfile. Instalações usam `npm ci`, scripts de instalação não revisados falham e o script opcional de `fsevents` é explicitamente negado porque o smoke HTTP não precisa dele. Nenhum download dinâmico de `latest` faz parte do quality gate.

O workflow tem `contents: read`, checkout com `persist-credentials: false`, timeout e concurrency. Não usa `pull_request_target`, secrets, `continue-on-error` ou etapas privilegiadas para pull requests externos.
