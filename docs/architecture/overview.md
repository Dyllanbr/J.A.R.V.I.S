# Arquitetura

## Estado

O backend é um único processo Go organizado como monólito modular. Por padrão ele permanece health-only e não exige banco; a Etapa 2B adiciona composição financeira opt-in, ainda aguardando revisão independente.

| Elemento | Estado |
| --- | --- |
| Monorepo | Implementado |
| Backend Go | Implementado, fundação e núcleo de transações |
| Health check HTTP | Implementado |
| Quality gate e OpenAPI 3.1 | Implementados |
| Baselines WCAG 2.2 AA e LGPD | Implementadas como documentação; conformidade não alegada |
| `transactions`: Money, Expense e CreateExpense | Verificado pela Etapa 1 |
| PostgreSQL 18.6 local/CI, migration 001 e adapter base | Verificados pela Etapa 2A |
| Audit event mínimo e atômico | Verificado pela Etapa 2A |
| Idempotência, migration 002 e API financeira | Implementados na Etapa 2B; revisão independente pendente |
| Preview e consulta mensal | Implementados na Etapa 2B; revisão independente pendente |
| Aplicativo SwiftUI/iOS | Planejado, sem projeto Xcode |
| Terraform/nuvem | Planejado, sem configuração |

## Direção de dependências

A composição acontece em `cmd/api` e `internal/app`. O adaptador HTTP fica em `internal/platform/httpserver`. A direção adotada para módulos de negócio é:

```text
transportes e persistência -> casos de uso -> domínio
              composição conecta dependências explícitas
```

Concretamente, HTTP e PostgreSQL dependem de casos de uso/ports de `application`, que dependem do domínio. `internal/platform/postgres` oferece configuração, pool e migrations ao adapter/comandos. Nem `domain` nem `application` importam HTTP ou PostgreSQL. Constraints SQL são defesa em profundidade e não deslocam regras financeiras para infraestrutura.

```text
httpapi adapter -> application -> domain
postgres adapter -> application ports + domain
composition root -> adapters + platform
```

`ExpenseCommandStore` existe porque idempotency record, Expense e AuditEvent precisam de uma única transação; não é um UnitOfWork genérico. `ExpenseReader` possui somente a consulta mensal já necessária. Preview reutiliza a canonicalização da aplicação/domínio sem gerar ID ou tocar persistência. A aplicação define UTC com precisão máxima de microssegundos como representação temporal da API financeira antes de preview, fingerprint e persistência, sem acoplar o domínio ao PostgreSQL.

Domínio e casos de uso não poderão importar HTTP, SQL, SDKs de IA ou integrações. Interfaces deverão nascer de necessidades reais dos casos de uso; não serão criadas antecipadamente. Handlers traduzirão entrada e saída e não conterão regras de negócio nem SQL.

## Limites dos módulos futuros

Cada módulo é um pacote coeso em `backend/internal/modules/<nome>`, com vocabulário e responsabilidades claros. `transactions/domain` não conhece aplicação ou infraestrutura; `transactions/application` depende do domínio e declara as portas mínimas que consome. Comunicação futura entre módulos ocorrerá por APIs internas explícitas. Compartilhamento será aceito apenas quando houver uma responsabilidade estável e nomeável; um pacote genérico `utils` não faz parte da arquitetura.

## Configuração e execução

A configuração vem do ambiente, é validada no início e não carrega `.env` implicitamente. A porta operacional deve estar entre 1 e 65535; porta dinâmica é restrita a harnesses controlados de teste. O endereço padrão usa apenas loopback. O servidor estabelece timeouts de cabeçalho, leitura, escrita e conexão ociosa, limite de cabeçalhos e desligamento gracioso limitado a 30 segundos. O primeiro `SIGINT` ou `SIGTERM` inicia o shutdown; um segundo sinal volta ao comportamento padrão e pode forçar o encerramento.

Startup registra apenas o endereço efetivamente ligado, shutdown registra o término e erros são reportados sem conteúdo de requisição ou configuração sensível. Logs não devem receber dados pessoais, financeiros, `DATABASE_URL` ou secrets. O processo usa configuração e caminhos relativos ao repositório. O container existente executa somente PostgreSQL local/teste; a aplicação Go ainda não é containerizada.

O pool PostgreSQL usa limites configuráveis conservadores (4 conexões máximas, 0 mínimas por padrão), timeout de conexão/ping e timeout por operação. Isso é baseline operacional, não tuning comprovado. Migrations e testes são comandos opt-in; ausência de configuração de banco não impede o startup health-only. Quando a API financeira é habilitada, `JARVIS_OWNER_ID` e PostgreSQL tornam-se obrigatórios, as rotas são registradas e o pool é fechado no shutdown.

O owner atual é um contexto single-owner temporário derivado pelo servidor, não autenticação. O cliente não controla owner, origin ou timezone. A API somente registra despesas já ocorridas no organizador e não movimenta fundos. A consulta mensal usa calendário IANA `America/Sao_Paulo`, limites `[start,end)` convertidos para UTC e ordenação total; UTC-3 não é hard-coded.

O domínio valida nomes de timezone pela base IANA do ambiente. `America/Sao_Paulo` é a baseline financeira planejada e UTC-3 não pode ser hard-coded. A disponibilidade de tzdata continua requisito operacional para um futuro container/deploy da aplicação; o container PostgreSQL não altera essa decisão.

## Evolução

Cada nova capacidade deve chegar em uma mudança vertical pequena: contrato, domínio, caso de uso, adaptadores e testes proporcionais ao risco. Tecnologias externas serão adicionadas somente quando houver um caso concreto e uma decisão registrada. Mudanças com dados pessoais exigem o [LGPD Readiness Gate](../privacy/privacy-by-design-checklist.md) quando houver beta externo; interfaces seguem a [baseline WCAG 2.2 AA](../accessibility/baseline.md).
