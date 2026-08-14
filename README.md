# J.A.R.V.I.S.

Monorepo do J.A.R.V.I.S., um assessor financeiro pessoal em construção. A Etapa 1 do Incremento 1 contém o núcleo verificado de uma despesa simples. A Etapa 2A adiciona migrations e persistência PostgreSQL real para dados sintéticos locais/de teste, aguardando revisão independente. O único comportamento HTTP continua sendo `GET /healthz`; não existe endpoint financeiro nem tela de produto.

## Estado atual

Implementado:

- backend Go em monólito modular;
- `Money`, `Expense` e `CreateExpense` verificados no núcleo do módulo `transactions`;
- migrations versionadas e adapter PostgreSQL para `Expense` + `EXPENSE_RECORDED` atômicos, implementados e aguardando revisão independente;
- health check operacional, configuração e shutdown gracioso;
- testes nativos Go e smoke de API com Playwright/TypeScript;
- contrato OpenAPI 3.1 validado semanticamente;
- quality gate local e CI, incluindo scanner de secrets com autoteste;
- baselines versionadas de arquitetura, segurança, privacidade, acessibilidade, QA e performance.

SwiftUI/iOS, Maestro, Terraform e infraestrutura de nuvem estão apenas planejados. O PostgreSQL existe somente para desenvolvimento e integração local/CI nesta etapa; não há ambiente ou fornecedor de produção.

## Estrutura

```text
.
├── apps/ios/                  # Aplicativo iOS planejado
├── backend/                   # Monólito modular, núcleo e adapter PostgreSQL
├── compose.yaml               # PostgreSQL local/teste, fixado por digest
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
- Git, Bash, Make e curl;
- Docker com Docker Compose para migrations e integração PostgreSQL.

Todas as dependências npm são instaladas pelo lockfile versionado. O repositório não depende de caminhos locais, instalações temporárias ou ambientes de desenvolvimento específicos.

## Bootstrap e validação

Em um checkout limpo, a partir da raiz:

```bash
make bootstrap
make check
```

`make check` executa as verificações rápidas de desenvolvimento sem iniciar containers. O quality gate completo, equivalente ao CI, faz uma instalação npm limpa e inclui build, race detector, PostgreSQL 18.6 real, migrations, auditoria, contrato, scanner e smoke gerenciado:

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

## PostgreSQL local e migrations

Exporte credenciais exclusivas para desenvolvimento local; não use dados ou senhas reais. O exemplo abaixo é deliberadamente fictício:

```bash
export JARVIS_POSTGRES_DB=jarvis_local
export JARVIS_POSTGRES_USER=jarvis_local
export JARVIS_POSTGRES_PASSWORD=choose-a-local-development-password
export JARVIS_POSTGRES_PORT=55432
export JARVIS_DATABASE_URL='postgres://jarvis_local:choose-a-local-development-password@127.0.0.1:55432/jarvis_local?sslmode=disable'

make db-up
make migrate-up
```

Para validar a persistência em um Postgres descartável e remover automaticamente container e volume:

```bash
make test-integration
```

`make db-down` encerra o banco de desenvolvimento preservando seu volume. `docker compose down --volumes` também remove esse volume quando a eliminação explícita dos dados sintéticos locais for desejada. Os comandos de banco são opt-in; a API health-only não lê `JARVIS_DATABASE_URL` e continua iniciando sem PostgreSQL.

## Smoke test

Após `make bootstrap`, execute da raiz:

```bash
make smoke
```

O alvo cria `backend/bin`, compila o binário atual, recusa porta previamente ocupada, inicia e aguarda somente esse processo, roda o Playwright e valida o shutdown por `SIGTERM`.

## Regras e evidências

As regras permanentes estão em [AGENTS.md](AGENTS.md). A [Definition of Done](docs/quality/definition-of-done.md) distingue itens planejados, implementados e verificados. Decisões arquiteturais estão nos [ADRs](docs/adr/README.md), e os gates futuros estão nas baselines de [acessibilidade](docs/accessibility/baseline.md) e [privacidade/LGPD](docs/privacy/README.md).

## Limitações atuais

O núcleo de despesa não está conectado à API nem a qualquer canal. Não há endpoint financeiro, idempotência, dados reais, banco de produção, receitas, parcelamentos, categorias funcionais, orçamento, metas, autenticação, Face ID, passkeys, PIN, WhatsApp funcional, OpenAI, IA, MCP, agentes de produto, cloud, Terraform funcional ou telas iOS. A persistência do audit event existe apenas junto à gravação da Expense; não há sistema de auditoria de produto além desse evento mínimo. A baseline LGPD não é alegação de conformidade jurídica, e a baseline WCAG não é alegação de conformidade de UI.
