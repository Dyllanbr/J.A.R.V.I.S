# J.A.R.V.I.S.

Fundação do monorepo do J.A.R.V.I.S., um assessor financeiro pessoal em construção. O repositório contém somente estrutura arquitetural, um processo HTTP mínimo com `GET /healthz`, contrato e automação de qualidade. Nenhuma funcionalidade financeira ou tela de produto foi implementada.

## Estado atual

Implementado:

- backend Go em monólito modular, ainda sem módulos de negócio;
- health check operacional, configuração e shutdown gracioso;
- testes nativos Go e smoke de API com Playwright/TypeScript;
- contrato OpenAPI 3.1 validado semanticamente;
- quality gate local e CI, incluindo scanner de secrets com autoteste;
- baselines versionadas de arquitetura, segurança, privacidade, acessibilidade, QA e performance.

PostgreSQL, SwiftUI/iOS, Maestro, Terraform e infraestrutura de nuvem estão apenas planejados. As pastas reservadas não significam implementação.

## Estrutura

```text
.
├── apps/ios/                  # Aplicativo iOS planejado
├── backend/                   # Fundação do monólito modular em Go
├── contracts/openapi/         # Contrato HTTP versionado
├── qa/
│   ├── playwright/            # Smoke de API em TypeScript
│   ├── maestro/               # Testes mobile planejados
│   ├── performance/           # Cenários de carga planejados
│   └── test-data/             # Política para dados sintéticos
├── infrastructure/terraform/  # Infraestrutura planejada, sem Terraform funcional
├── scripts/                   # Verificações compartilhadas por execução local e CI
└── docs/                      # Arquitetura e requisitos de qualidade
```

## Pré-requisitos reproduzíveis

- Go 1.26.6, registrado em `.go-version` e `backend/go.mod`;
- Node.js 24.19.0, registrado em `.node-version`;
- npm 11.17.0, registrado no `packageManager` da suíte Playwright;
- Git, Bash, Make e curl.

Todas as dependências npm são instaladas pelo lockfile versionado. O repositório não depende de caminhos locais, instalações temporárias ou ambientes de desenvolvimento específicos.

## Bootstrap e validação

Em um checkout limpo, a partir da raiz:

```bash
make bootstrap
make check
```

`make check` executa as verificações rápidas de desenvolvimento. O quality gate completo, equivalente ao CI, faz uma instalação npm limpa e inclui build, race detector, auditoria, contrato, scanner e smoke gerenciado:

```bash
make verify
```

## Executar a API

```bash
cd backend
go run ./cmd/api
```

O servidor escuta em `127.0.0.1:8080` por padrão. Para alterar a configuração, exporte as variáveis no shell ou passe-as ao comando:

```bash
JARVIS_HTTP_ADDRESS=127.0.0.1:8081 JARVIS_SHUTDOWN_TIMEOUT=10s go run ./cmd/api
```

`.env.example` é apenas uma referência de nomes e valores fictícios; o binário não carrega arquivos `.env` automaticamente.

## Smoke test

Após `make bootstrap`, execute da raiz:

```bash
make smoke
```

O alvo cria `backend/bin`, compila o binário atual, recusa porta previamente ocupada, inicia e aguarda somente esse processo, roda o Playwright e valida o shutdown por `SIGTERM`.

## Regras e evidências

As regras permanentes estão em [AGENTS.md](AGENTS.md). A [Definition of Done](docs/quality/definition-of-done.md) distingue itens planejados, implementados e verificados. Decisões arquiteturais estão nos [ADRs](docs/adr/README.md), e os gates futuros estão nas baselines de [acessibilidade](docs/accessibility/baseline.md) e [privacidade/LGPD](docs/privacy/README.md).

## Limitações atuais

Não há despesas, receitas, parcelamentos, categorias funcionais, orçamento, metas, banco de dados, autenticação, Face ID, passkeys, PIN, WhatsApp, OpenAI, IA, MCP, agentes de produto, cloud, Terraform funcional ou telas iOS. A baseline LGPD não é alegação de conformidade jurídica, e a baseline WCAG não é alegação de conformidade de UI.
