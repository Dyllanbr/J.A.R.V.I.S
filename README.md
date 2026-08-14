# J.A.R.V.I.S.

Monorepo do J.A.R.V.I.S., um assessor financeiro pessoal em construção. O núcleo, a persistência PostgreSQL e a Financial API da despesa simples estão verificados pelas etapas anteriores. A Etapa 3 implementa o primeiro app SwiftUI nativo, aguardando revisão independente. O sistema apenas registra informações no organizador: não executa Pix, pagamentos, compras, transferências ou qualquer movimentação de fundos.

## Estado atual

Implementado:

- backend Go em monólito modular;
- `Money`, `Expense` e `CreateExpense` verificados no núcleo do módulo `transactions`;
- migrations versionadas e adapter PostgreSQL verificados para `Expense` + `EXPENSE_RECORDED` atômicos;
- API financeira opt-in com preview sem escrita, criação idempotente e listagem mensal, verificada pela Etapa 2B;
- projeto iOS 17/SwiftUI, entrada com preview/revisão/confirmação e histórico mensal, implementados e aguardando revisão independente;
- XCTest/XCUITest e integração automatizada Simulator → API → PostgreSQL, implementados;
- health check operacional, configuração e shutdown gracioso;
- testes nativos Go e smoke de API com Playwright/TypeScript;
- contrato OpenAPI 3.1 validado semanticamente;
- quality gate local e CI, incluindo scanner de secrets com autoteste;
- baselines versionadas de arquitetura, segurança, privacidade, acessibilidade, QA e performance.

Maestro, dispositivo físico, autenticação, armazenamento local seguro, Terraform e infraestrutura de nuvem estão apenas planejados. O PostgreSQL existe somente para desenvolvimento e integração local/CI nesta etapa; não há ambiente ou fornecedor de produção.

## Estrutura

```text
.
├── apps/ios/                  # Projeto SwiftUI, XCTest e XCUITest
├── backend/                   # Monólito modular, núcleo e adapter PostgreSQL
├── compose.yaml               # PostgreSQL local/teste, fixado por digest
├── contracts/openapi/         # Contrato HTTP operacional e financeiro versionado
├── qa/
│   ├── playwright/            # Smoke e E2E financeiro em TypeScript
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
- Docker com Docker Compose para migrations e integração PostgreSQL;
- Para o gate iOS: macOS, Xcode e um iPhone Simulator com runtime compatível.

Todas as dependências npm são instaladas pelo lockfile versionado. O repositório não depende de caminhos locais, instalações temporárias ou ambientes de desenvolvimento específicos.

## Bootstrap e validação

Em um checkout limpo, a partir da raiz:

```bash
make bootstrap
make check
```

`make check` executa as verificações rápidas de desenvolvimento sem iniciar containers. O quality gate completo, equivalente ao CI, faz uma instalação npm limpa e inclui build, race detector, PostgreSQL 18.6 real, migrations, idempotência concorrente, E2E financeiro, auditoria, contrato, scanner e smoke health-only gerenciado:

```bash
make verify
```

## Executar a API

```bash
cd backend
go run ./cmd/api
```

O servidor escuta em `127.0.0.1:8080` por padrão. Sem configuração adicional, somente `/healthz` é registrado e nenhum pool PostgreSQL é aberto. Para alterar a configuração operacional, exporte as variáveis no shell ou passe-as ao comando:

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

Para validar persistência, migrations, concorrência idempotente e endpoints via Playwright em um Postgres descartável, removendo automaticamente API, container e volume:

```bash
make test-integration
```

`make db-down` encerra o banco de desenvolvimento preservando seu volume. `docker compose down --volumes` também remove esse volume quando a eliminação explícita dos dados sintéticos locais for desejada. Os comandos de banco são opt-in; a API health-only não lê `JARVIS_DATABASE_URL` e continua iniciando sem PostgreSQL.

Para executar a API financeira local, aplique as migrations, crie um owner exclusivamente sintético e habilite explicitamente o contexto single-owner temporário:

```bash
docker compose exec -T postgres psql \
  --username "$JARVIS_POSTGRES_USER" \
  --dbname "$JARVIS_POSTGRES_DB" \
  --command "INSERT INTO users (id, created_at, updated_at) VALUES ('usr_local_synthetic_owner', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (id) DO NOTHING"

export JARVIS_FINANCIAL_API_ENABLED=true
export JARVIS_OWNER_ID=usr_local_synthetic_owner
cd backend
go run ./cmd/api
```

Esse owner vem do servidor e não é autenticação. O contrato expõe `POST /v1/transactions/preview`, `POST /v1/transactions` e `GET /v1/transactions?month=YYYY-MM`. O POST mutável deve ser chamado pelo canal somente depois de apresentar o preview e obter confirmação explícita. `origin=IOS` e `America/Sao_Paulo` são atribuídos pelo servidor; o cliente não envia `userId`, origin ou timezone.

## Aplicativo iOS

No macOS com Xcode e um iPhone Simulator disponível:

```bash
make build-ios
make verify-ios
make test-ios-integration
```

O gate iOS é separado de `make verify`, que continua reproduzível no ambiente cross-platform do backend. XCUITest com stub cobre regressão de UI; a integração local real é fail-closed, gerencia PostgreSQL, migrations, owner/fixture sintéticos, API e Simulator, passa pelo app/URLSession e exige no banco uma Expense, um audit event e um registro idempotente concluído. Instruções de configuração e as limitações de segurança estão em [`apps/ios/README.md`](apps/ios/README.md).

## Smoke test

Após `make bootstrap`, execute da raiz:

```bash
make smoke
```

O alvo cria `backend/bin`, compila o binário atual, recusa porta previamente ocupada, inicia e aguarda somente esse processo, roda o Playwright e valida o shutdown por `SIGTERM`.

## Regras e evidências

As regras permanentes estão em [AGENTS.md](AGENTS.md). A [Definition of Done](docs/quality/definition-of-done.md) distingue itens planejados, implementados e verificados. Decisões arquiteturais estão nos [ADRs](docs/adr/README.md), e os gates futuros estão nas baselines de [acessibilidade](docs/accessibility/baseline.md) e [privacidade/LGPD](docs/privacy/README.md).

## Limitações atuais

A API financeira é um contexto local single-owner temporário, sem autenticação, autorização multiusuário, rate limiting distribuído ou uso real aprovado. O app iOS não persiste dados localmente, e o retry idempotente pendente não sobrevive a restart. Não há dados reais, banco de produção, receitas, parcelamentos, categorias funcionais, orçamento, metas, Face ID, passkeys, PIN, WhatsApp funcional, OpenAI, IA, MCP, agentes de produto, cloud ou Terraform funcional. Dispositivo físico/LAN permanece planejado até existir proteção apropriada. O audit event existe apenas junto ao novo registro; preview, replay e leitura não geram eventos. A retenção de metadata de idempotência e outcomes de commit indeterminado exigem política operacional antes de uso real. As baselines LGPD e WCAG não são alegações de conformidade.
