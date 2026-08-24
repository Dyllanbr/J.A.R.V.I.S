# Arquitetura

## Estado

O backend é um único processo Go organizado como monólito modular. Por padrão ele permanece health-only e não exige banco. Os Incrementos 1 — Despesas, 2 — Receitas, 3A — Categorias e filtros do histórico e 3B — Recorrências confirmadas e assinaturas estão verificados. O Incremento 3C — Detecção e sugestão de recorrências permanece planejado e não faz parte da arquitetura implementada atual.

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
| `Income`, migration 003 e persistência/auditoria idempotente | Verificados pelo Incremento 2 |
| Projeção mensal mista `MonthlyTransaction` e API discriminada | Verificadas pelo Incremento 2 |
| Aplicativo SwiftUI/iOS 17 para Expense | Verificado pelo Incremento 1 |
| Suporte iOS a Income e histórico misto | Verificado pelo Incremento 2 |
| XCTest, XCUITest e integração Simulator/API/PostgreSQL para Expense/Income | Verificados pelos Incrementos 1 e 2 |
| Category opcional, migration 004 e catálogo PostgreSQL | Verificados pelo Incremento 3A |
| Descoberta de categorias e filtros locais no iOS | Verificados pelo Incremento 3A |
| `Recurrence`, migration 005 e persistência/auditoria/idempotência dedicadas | Verificados pelo Incremento 3B |
| API REST/OpenAPI e terceira área iOS para Recorrências | Verificadas pelo Incremento 3B |
| Testes de Recurrence, integração PostgreSQL, Playwright, XCTest, XCUITest e E2E real | Verificados pelo Incremento 3B |
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

`Recurrence` é um terceiro agregado financeiro, mas representa uma expectativa recorrente e não uma transação ocorrida. No Incremento 3B ele é restrito a despesas recorrentes confirmadas manualmente, frequência mensal, valor esperado positivo e `startsOn` como data civil. Seu lifecycle é `ACTIVE → CANCELLED`, terminal nesta versão. Ele não possui `Category`, `PaymentMethod` ou `OccurredAt`, não é uma variante de `MonthlyTransaction` e nunca cria `Expense` ou `Income` automaticamente. Criação e cancelamento possuem idempotência e auditoria próprias. A aplicação faz preflight de replay antes de consumir ID/Clock, enquanto a persistência continua sendo a autoridade final para corridas concorrentes.

PostgreSQL é o source of truth runtime do catálogo global de sistema. A migration 004 cria `categories`, adiciona `transactions.category_id` nullable e usa FK composta para defender a relação entre tipo financeiro e Category. Não existe CRUD ou reclassificação. O DOWN obtém lock exclusivo antes do guard para impedir perda concorrente de classificação. A API expõe `GET /v1/categories`; preview, create e histórico transportam somente a key técnica opcional, nunca display name fornecido pelo cliente.

A migration 005 adiciona persistência dedicada de recorrências em `recurrences`, `recurrence_audit_events` e `recurrence_idempotency_records`, sem alterar as tabelas financeiras legadas nem produzir efeitos colaterais em Expense/Income. Datas civis são transportadas por tipos PostgreSQL de `DATE`, sem depender de `DATE::text` ou `DateStyle`. O replay de criação preserva um snapshot histórico relacional, de modo que repetir a mesma criação após um cancelamento posterior ainda devolve a resposta histórica original sem alterar o estado corrente `CANCELLED`. O DOWN possui guardas e cobertura concorrente para evitar remoção insegura.

O cliente iOS segue uma direção igualmente curta:

```text
SwiftUI Views -> View Models -> FinancialAPI -> URLSession -> backend
```

Views não montam JSON; features dependem da abstração pequena `FinancialAPI`, e o cliente concreto concentra DTOs/HTTP discriminados por `EXPENSE`/`INCOME` e os contratos próprios de `Recurrence`. A composição fica em `JARVISApp`/`AppModel`, sem singleton ou container de DI. `CategoryCatalogModel` pertence ao `AppModel`, mantém uma única Task compartilhada de catálogo e não transfere ownership do fetch para as Tasks efêmeras das Views. Parsing BRL inteiro, codec temporal e data civil de Recurrence têm responsabilidades nomeadas. O preview devolvido pelo servidor congela a semântica revisada antes da confirmação; alterações no draft invalidam respostas antigas por geração. A mesma chave idempotente permanece em memória durante retries transitórios; edição ou troca de tipo inicia nova tentativa, e erros determinísticos `400`/`409` retornam à edição em vez de oferecer retry infinito.

`RecurrencesViewModel` mantém carregamento single-flight de lista sem transferir o ownership da Task para callers efêmeros. Resultados de CREATE/CANCEL não podem ser sobrescritos por uma listagem iniciada antes da mutação: revisões e gerações de carregamento detectam respostas obsoletas e, quando necessário, disparam reconciliação autoritativa posterior. Erros autoritativos de cancelamento não fabricam `cancelledAt` nem removem itens localmente.

O harness iOS possui dois modos explícitos: stub `DEBUG` para regressão de UI e real para Simulator → app → URLSession → backend → PostgreSQL. O modo real não possui fallback e exige uma pós-condição no banco. Em `RootView`, um `UITabBarController` nativo hospeda Register, History e Recurrences em controllers SwiftUI separados; cada controller possui seu próprio `UITabBarItem` e identifier semântico, sem associação por posição, copy, símbolo ou temporização. O terceiro tab usa `tab.recurrences` e preserva a mesma estratégia de instrumentação acessível das áreas anteriores.

Domínio e casos de uso não poderão importar HTTP, SQL, SDKs de IA ou integrações. Interfaces deverão nascer de necessidades reais dos casos de uso; não serão criadas antecipadamente. Handlers traduzirão entrada e saída e não conterão regras de negócio nem SQL.

## Limites dos módulos futuros

Cada módulo é um pacote coeso em `backend/internal/modules/<nome>`, com vocabulário e responsabilidades claros. `transactions/domain` não conhece aplicação ou infraestrutura; `transactions/application` depende do domínio e declara as portas mínimas que consome. Comunicação futura entre módulos ocorrerá por APIs internas explícitas. Compartilhamento será aceito apenas quando houver uma responsabilidade estável e nomeável; um pacote genérico `utils` não faz parte da arquitetura.

## Configuração e execução

A configuração vem do ambiente, é validada no início e não carrega `.env` implicitamente. A porta operacional deve estar entre 1 e 65535; porta dinâmica é restrita a harnesses controlados de teste. O endereço padrão usa apenas loopback. O servidor estabelece timeouts de cabeçalho, leitura, escrita e conexão ociosa, limite de cabeçalhos e desligamento gracioso limitado a 30 segundos. O primeiro `SIGINT` ou `SIGTERM` inicia o shutdown; um segundo sinal volta ao comportamento padrão e pode forçar o encerramento.

Startup registra apenas o endereço efetivamente ligado, shutdown registra o término e erros são reportados sem conteúdo de requisição ou configuração sensível. Logs não devem receber dados pessoais, financeiros, `DATABASE_URL` ou secrets. O processo usa configuração e caminhos relativos ao repositório. O container existente executa somente PostgreSQL local/teste; a aplicação Go ainda não é containerizada.

O pool PostgreSQL usa limites configuráveis conservadores (4 conexões máximas, 0 mínimas por padrão), timeout de conexão/ping e timeout por operação. Isso é baseline operacional, não tuning comprovado. Migrations e testes são comandos opt-in; ausência de configuração de banco não impede o startup health-only. Quando a API financeira é habilitada, `JARVIS_OWNER_ID` e PostgreSQL tornam-se obrigatórios, as rotas são registradas e o pool é fechado no shutdown.

O owner atual é um contexto single-owner temporário derivado pelo servidor, não autenticação. O cliente não controla owner, origin ou timezone. A API registra despesas e receitas já ocorridas no organizador e também gerencia recorrências confirmadas como compromissos esperados separados; ela não recebe nem movimenta fundos. A consulta mensal de transações retorna itens discriminados com `categoryId` opcional, sem totais, saldo, orçamento ou Disponível Seguro; usa calendário IANA `America/Sao_Paulo`, limites `[start,end)` convertidos para UTC e ordenação total. Os filtros de tipo e Category são client-side no iOS; a API mensal não possui filtros, search ou agrupamento. As rotas de Recurrence são dedicadas a preview, criação, listagem e cancelamento e não criam transações como efeito colateral. UTC-3 não é hard-coded.

O domínio valida nomes de timezone pela base IANA do ambiente. `America/Sao_Paulo` é a baseline financeira planejada e UTC-3 não pode ser hard-coded. A disponibilidade de tzdata continua requisito operacional para um futuro container/deploy da aplicação; o container PostgreSQL não altera essa decisão.

## Evolução

Cada nova capacidade deve chegar em uma mudança vertical pequena: contrato, domínio, caso de uso, adaptadores e testes proporcionais ao risco. Tecnologias externas serão adicionadas somente quando houver um caso concreto e uma decisão registrada. Mudanças com dados pessoais exigem o [LGPD Readiness Gate](../privacy/privacy-by-design-checklist.md) quando houver beta externo; interfaces seguem a [baseline WCAG 2.2 AA](../accessibility/baseline.md).
