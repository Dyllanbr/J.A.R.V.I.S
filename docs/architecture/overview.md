# Arquitetura

## Estado

O backend é um único processo Go organizado como monólito modular. Por padrão ele permanece health-only e não exige banco. O Incremento 1 — Despesas está verificado. O Incremento 2 — Receitas estende o mesmo módulo e os mesmos endpoints, está implementado e pronto para auditoria global independente. O Incremento 3A — Categorias e filtros do histórico está implementado e aguarda auditoria final independente.

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
| Idempotência, migration 002, preview e API financeira | Verificados pela Etapa 2B |
| Consulta mensal owner-scoped | Verificada pela Etapa 2B |
| `Income`, migration 003 e persistência/auditoria idempotente | Implementados pelo Incremento 2; auditoria global pendente |
| Projeção mensal mista `MonthlyTransaction` e API discriminada | Implementadas pelo Incremento 2; auditoria global pendente |
| Aplicativo SwiftUI/iOS 17 para Expense | Verificado pelo Incremento 1 |
| Suporte iOS a Income e histórico misto | Implementado pelo Incremento 2; auditoria global pendente |
| XCTest, XCUITest e integração Simulator/API/PostgreSQL | Implementados para ambos os tipos; verificação final do Incremento 2 pendente |
| Category opcional, migration 004 e catálogo PostgreSQL | Implementados pelo Incremento 3A; auditoria final pendente |
| Descoberta de categorias e filtros locais no iOS | Implementados pelo Incremento 3A; auditoria final pendente |
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

`Expense` e `Income` são agregados de escrita separados e usam portas específicas `ExpenseCommandStore`/`IncomeCommandStore`, pois idempotency record, agregado e AuditEvent precisam de uma única transação; elas não formam um UnitOfWork genérico. Ambos aceitam `CategoryID` opcional: ausência significa “Sem categoria”, não “Outros”. A aplicação consome a port read-only `CategoryCatalog` para validar existência e applicability, sem inferir tipo pelo prefixo do ID. Category presente integra o fingerprint; ausência preserva o fingerprint legado. `MonthlyTransaction` é somente uma projeção de leitura discriminada e não uma entidade universal de escrita. Preview reutiliza a canonicalização da aplicação/domínio sem gerar ID ou tocar persistência. A aplicação define UTC com precisão máxima de microssegundos como representação temporal da API financeira antes de preview, fingerprint e persistência, sem acoplar o domínio ao PostgreSQL.

PostgreSQL é o source of truth runtime do catálogo global de sistema. A migration 004 cria `categories`, adiciona `transactions.category_id` nullable e usa FK composta para defender a relação entre tipo financeiro e Category. Não existe CRUD ou reclassificação. O DOWN obtém lock exclusivo antes do guard para impedir perda concorrente de classificação. A API expõe `GET /v1/categories`; preview, create e histórico transportam somente a key técnica opcional, nunca display name fornecido pelo cliente.

O cliente iOS segue uma direção igualmente curta:

```text
SwiftUI Views -> View Models -> FinancialAPI -> URLSession -> backend
```

Views não montam JSON; features dependem da abstração pequena `FinancialAPI`, e o cliente concreto concentra DTOs/HTTP discriminados por `EXPENSE`/`INCOME`. A composição fica em `JARVISApp`/`AppModel`, sem singleton ou container de DI. `CategoryCatalogModel` pertence ao `AppModel`, mantém uma única Task compartilhada de catálogo e não transfere ownership do fetch para as Tasks efêmeras das Views. Parsing BRL inteiro e codec temporal têm responsabilidades nomeadas. O preview devolvido pelo servidor congela a semântica revisada antes da confirmação; alterações no draft invalidam respostas antigas por geração. A mesma chave idempotente permanece em memória durante retries transitórios; edição ou troca de tipo inicia nova tentativa, e erros determinísticos `400`/`409` retornam à edição em vez de oferecer retry infinito.

O harness iOS possui dois modos explícitos: stub `DEBUG` para regressão de UI e real para Simulator → app → URLSession → backend → PostgreSQL. O modo real não possui fallback e exige uma pós-condição no banco. Em `RootView`, um `UITabBarController` nativo hospeda Register e History em controllers SwiftUI separados; cada controller possui seu próprio `UITabBarItem` e identifier semântico, sem associação por posição, copy, símbolo ou temporização.

Domínio e casos de uso não poderão importar HTTP, SQL, SDKs de IA ou integrações. Interfaces deverão nascer de necessidades reais dos casos de uso; não serão criadas antecipadamente. Handlers traduzirão entrada e saída e não conterão regras de negócio nem SQL.

## Limites dos módulos futuros

Cada módulo é um pacote coeso em `backend/internal/modules/<nome>`, com vocabulário e responsabilidades claros. `transactions/domain` não conhece aplicação ou infraestrutura; `transactions/application` depende do domínio e declara as portas mínimas que consome. Comunicação futura entre módulos ocorrerá por APIs internas explícitas. Compartilhamento será aceito apenas quando houver uma responsabilidade estável e nomeável; um pacote genérico `utils` não faz parte da arquitetura.

## Configuração e execução

A configuração vem do ambiente, é validada no início e não carrega `.env` implicitamente. A porta operacional deve estar entre 1 e 65535; porta dinâmica é restrita a harnesses controlados de teste. O endereço padrão usa apenas loopback. O servidor estabelece timeouts de cabeçalho, leitura, escrita e conexão ociosa, limite de cabeçalhos e desligamento gracioso limitado a 30 segundos. O primeiro `SIGINT` ou `SIGTERM` inicia o shutdown; um segundo sinal volta ao comportamento padrão e pode forçar o encerramento.

Startup registra apenas o endereço efetivamente ligado, shutdown registra o término e erros são reportados sem conteúdo de requisição ou configuração sensível. Logs não devem receber dados pessoais, financeiros, `DATABASE_URL` ou secrets. O processo usa configuração e caminhos relativos ao repositório. O container existente executa somente PostgreSQL local/teste; a aplicação Go ainda não é containerizada.

O pool PostgreSQL usa limites configuráveis conservadores (4 conexões máximas, 0 mínimas por padrão), timeout de conexão/ping e timeout por operação. Isso é baseline operacional, não tuning comprovado. Migrations e testes são comandos opt-in; ausência de configuração de banco não impede o startup health-only. Quando a API financeira é habilitada, `JARVIS_OWNER_ID` e PostgreSQL tornam-se obrigatórios, as rotas são registradas e o pool é fechado no shutdown.

O owner atual é um contexto single-owner temporário derivado pelo servidor, não autenticação. O cliente não controla owner, origin ou timezone. A API somente registra despesas e receitas já ocorridas no organizador e não recebe nem movimenta fundos. A consulta mensal retorna itens discriminados com `categoryId` opcional, sem totais, saldo, orçamento ou Disponível Seguro; usa calendário IANA `America/Sao_Paulo`, limites `[start,end)` convertidos para UTC e ordenação total. Os filtros de tipo e Category são client-side no iOS; a API mensal não possui filtros, search ou agrupamento. UTC-3 não é hard-coded.

O domínio valida nomes de timezone pela base IANA do ambiente. `America/Sao_Paulo` é a baseline financeira planejada e UTC-3 não pode ser hard-coded. A disponibilidade de tzdata continua requisito operacional para um futuro container/deploy da aplicação; o container PostgreSQL não altera essa decisão.

## Evolução

Cada nova capacidade deve chegar em uma mudança vertical pequena: contrato, domínio, caso de uso, adaptadores e testes proporcionais ao risco. Tecnologias externas serão adicionadas somente quando houver um caso concreto e uma decisão registrada. Mudanças com dados pessoais exigem o [LGPD Readiness Gate](../privacy/privacy-by-design-checklist.md) quando houver beta externo; interfaces seguem a [baseline WCAG 2.2 AA](../accessibility/baseline.md).
