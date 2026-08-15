# Módulo de transações

## Estado atual

| Item | Estado | Evidência candidata |
| --- | --- | --- |
| `Money` em minor units inteiras com BRL | VERIFICADO | Etapa 1 mergeada, testes unitários/fuzz e revisão independente |
| Entidade `Expense` e invariantes | VERIFICADO | Etapa 1 mergeada, testes unitários/fuzz e revisão independente |
| Caso de uso `CreateExpense` | VERIFICADO | Etapa 1 mergeada, testes determinísticos e revisão independente |
| Persistência PostgreSQL base | VERIFICADO | Etapa 2A mergeada, integração real e revisão independente |
| Migration 001 e `AuditEvent` transacional | VERIFICADO | Etapa 2A mergeada, atomicidade e lifecycle revisados |
| Idempotência e migration 002 | VERIFICADO | Etapa 2B mergeada, integração/concurrency e revisão independente |
| Preview e API financeira | VERIFICADO | Etapa 2B mergeada, contrato/E2E e revisão independente |
| Consulta mensal | VERIFICADA | Etapa 2B mergeada, query owner-scoped e limites financeiros revisados |
| Cliente iOS para Expense | VERIFICADO | Incremento 1 mergeado e aprovado após revisão independente |
| `Income`, migration 003 e persistência/auditoria idempotente | IMPLEMENTADO | Incremento 2 com stages auditadas; auditoria global pendente |
| API discriminada e histórico mensal Expense + Income | IMPLEMENTADO | Incremento 2 com contrato, PostgreSQL e E2E reais; auditoria global pendente |
| Cliente iOS para Income e histórico misto | IMPLEMENTADO | Incremento 2 com XCTest/XCUITest/E2E; auditoria global pendente |
| Demais canais | PLANEJADO | não implementados |

O estado **VERIFICADO** depende de quality gate e revisão independente conforme a Definition of Done; não é atribuído autonomamente por esta implementação.

## Domínio

`domain` não importa HTTP, JSON, SQL, banco de dados ou integrações. `Money` armazena somente um `int64` de minor units. Como BRL é a única moeda desta etapa, `Currency()` retorna BRL e o zero value de `Money` representa validamente R$ 0,00 BRL; `NewMoney` ainda rejeita qualquer moeda solicitada diferente de BRL. A igualdade compara os minor units, pois toda instância possui a mesma moeda implícita. R$ 42,50 é representado por `4250`. `Money` aceita valores assinados e zero como valor monetário genérico; os agregados `Expense` e `Income` exigem magnitude estritamente positiva. A direção vem exclusivamente de `type`, nunca do sinal.

`Expense` e `Income` são agregados de escrita explícitos, não variantes de uma entidade universal `Transaction`. Ambos possuem identificadores, tipo fixo, descrição, valor, instante de ocorrência, timezone financeiro IANA, origem, status, versão e timestamps. Somente `Expense` possui forma de pagamento; `Income` não aceita esse campo. ID e UserID são identificadores opacos: devem usar UTF-8 válido, ter de 1 a 128 bytes, não podem conter caracteres de controle nem espaços externos e não precisam seguir UUID. A descrição tem espaços externos removidos, deve conter UTF-8 válido, não pode ser vazia e possui limite de 200 caracteres Unicode. Seu conteúdo interno, incluindo multibyte, emoji, combining marks, ZWJ e whitespace interno, não é normalizado. Uma eventual regra de produto para caracteres visualmente vazios, como U+200B, permanece planejada e não é inferida nesta etapa.

As formas aceitas são `PIX`, `DEBIT`, `CREDIT` e `CASH`. As origens aceitas são `IOS` e `WHATSAPP`; isso apenas define o vocabulário do domínio e não implementa nenhum canal. O estado inicial é `RECORDED`, com versão `1`, porque o caso de uso somente deve ser invocado depois da confirmação explícita no canal chamador.

O instante é normalizado para UTC e o timezone financeiro permanece explícito. `America/Sao_Paulo` é a baseline planejada do produto, mas UTC-3 não é hard-coded pelo domínio. `Local` é rejeitado porque depende do ambiente do processo. Ambientes futuros que executem a validação IANA deverão fornecer tzdata; isso será validado antes de containerização ou deploy, sem embutir `time/tzdata` nesta etapa.

## Aplicação e portas

`application.CreateExpense` permanece como caso de uso verificado do Incremento 1. Os fluxos HTTP atuais usam `PreviewExpense`/`RecordExpense` e `PreviewIncome`/`RecordIncome`; não existe `confirmed=true`. Cada interface é responsável por não registrar antes da confirmação e o cliente iOS cumpre preview → revisão congelada → confirmação explícita para ambos os tipos.

Os dois Previews reutilizam normalização/canonicalização sem ID, Clock ou persistência. Instantes financeiros são convertidos para UTC e truncados para microssegundos antes de preview, fingerprint, criação persistível e resposta; o Clock passa pela mesma canonicalização para `createdAt`/`updatedAt`. `RecordExpense` e `RecordIncome` delegam atomicidade às portas pequenas `ExpenseCommandStore` e `IncomeCommandStore`. `ListTransactionsByMonth` calcula `[start,end)` em `America/Sao_Paulo` e retorna `MonthlyTransaction`, uma projeção discriminada de leitura; ela não é agregado nem comando de escrita. As portas continuam específicas; não há UnitOfWork, repository genérico ou dependência de pgx/HTTP na aplicação.

## Adapter PostgreSQL

Os command stores executam uma DB transaction única: reservam a idempotência, inserem a transaction, inserem o evento mínimo correspondente, concluem a reserva e fazem commit. Expense usa `CREATE_EXPENSE`/`EXPENSE_RECORDED`; Income usa `CREATE_INCOME`/`INCOME_RECORDED`. Qualquer falha provoca rollback. O evento guarda owner lógico, aggregate, versão, tipo e instante; não replica descrição, valor ou payload financeiro. FKs compostas preservam ownership e impedem concluir uma operação apontando para o tipo financeiro errado.

O schema também protege invariantes essenciais com PK, FK, `UNIQUE` e `CHECK`, mas o domínio continua sendo a autoridade. Para os limites de borda, a migration enumera explicitamente o conjunto Unicode White_Space usado por `strings.TrimSpace` no Go 1.26.6: IDs e timezones com whitespace externo são rejeitados, descrições persistidas devem estar sem whitespace externo e descrições formadas somente por esse conjunto são inválidas. Whitespace interno permanece intacto e nenhuma normalização Unicode é feita. Como a tabela Unicode do Go pode evoluir em uma atualização futura da toolchain, esse conjunto SQL deverá ser revisto junto com a versão do Go; não há dependência de locale ou extensão PostgreSQL.

Instantes usam `TIMESTAMPTZ`, enquanto `financial_timezone` preserva o identificador IANA. A migration 003 permite apenas `EXPENSE` e `INCOME`: Expense exige um payment method permitido e Income exige `payment_method IS NULL`; `amount_minor` permanece estritamente positivo para ambos. A tabela `users` contém somente ID e timestamps para ownership/referential integrity; ela não implementa autenticação e as migrations não contêm usuários.

## API e idempotência

O adapter HTTP expõe preview, registro e consulta mensal somente quando `JARVIS_FINANCIAL_API_ENABLED=true`. O owner vem de `JARVIS_OWNER_ID` no servidor como contexto single-owner temporário; não é autenticação e nunca é aceito ou retornado pelo contrato. A API atribui `origin=IOS` e `financialTimezone=America/Sao_Paulo`. O decoder rejeita propriedades desconhecidas e JSON adicional, o body é limitado, Money usa minor units inteiras e respostas financeiras desabilitam cache.

`idempotency_records` guarda apenas metadata técnica: owner, operação, chave, SHA-256 da representação canônica, estado, referência da transaction e timestamps. Não replica request, descrição, amount ou response. A chave é escopada por `(owner, operation, key)`, com operações separadas `CREATE_EXPENSE` e `CREATE_INCOME`; por isso a mesma string pode existir para os dois tipos. Replay lê o recurso original, payload diferente retorna conflito e rollback remove a reserva. PostgreSQL é a autoridade para concorrência e atomicidade com o audit event. Um resultado de commit indeterminado nunca deve ser tratado como sucesso e reforça a obrigação de retry com a mesma chave.

O fingerprint inclui tipo, descrição normalizada, Money, instante já canonicalizado em UTC/microssegundos, origin e timezone atribuídos pelo servidor; somente Expense inclui forma de pagamento. Duas entradas que diferem apenas na precisão descartada são semanticamente equivalentes; instantes canônicos distintos geram fingerprints distintos. Exclui ID gerado, Clock e timestamps de infraestrutura. A implementação atual ainda gera ID e consulta Clock antes de o store identificar um replay de Income; isso não altera o recurso persistido nem a garantia idempotente. `GET` filtra owner, timezone e intervalo mensal inclusivo/exclusivo, ordenando por `occurred_at DESC, id DESC`; o índice `(user_id, occurred_at DESC, id DESC)` atende à query mista sem índice especulativo por tipo.

## Decisões adiadas

Retenção de metadata idempotente, autenticação real, rate limiting distribuído e tratamento de outcomes operacionais indeterminados permanecem decisões anteriores ao uso real. Idempotência não faz parte de `Money`, `Expense` ou `Income`, e logs não substituem audit events nem recebem conteúdo financeiro.

Não existem autenticação, armazenamento financeiro local, categorias, recorrências, orçamento, saldo calculado, Disponível Seguro, IA, WhatsApp funcional, Open Finance, infraestrutura de nuvem ou banco de produção nesta etapa. A UI iOS usa dados sintéticos em desenvolvimento. A API registra que uma despesa ou receita já ocorreu; nunca executa recebimento, Pix, pagamento, compra ou transferência.
