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
| Cliente iOS | IMPLEMENTADO | Etapa 3 aguardando revisão independente |
| Demais canais | PLANEJADO | não implementados |

O estado **VERIFICADO** depende de quality gate e revisão independente conforme a Definition of Done; não é atribuído autonomamente por esta implementação.

## Domínio

`domain` não importa HTTP, JSON, SQL, banco de dados ou integrações. `Money` armazena somente um `int64` de minor units. Como BRL é a única moeda desta etapa, `Currency()` retorna BRL e o zero value de `Money` representa validamente R$ 0,00 BRL; `NewMoney` ainda rejeita qualquer moeda solicitada diferente de BRL. A igualdade compara os minor units, pois toda instância possui a mesma moeda implícita. R$ 42,50 é representado por `4250`. `Money` aceita valores assinados e zero como valor monetário genérico; uma `Expense` exige valor estritamente positivo.

Uma despesa possui identificadores, tipo fixo `EXPENSE`, descrição, valor, forma de pagamento, instante de ocorrência, timezone financeiro IANA, origem, status, versão e timestamps. ID e UserID são identificadores opacos: devem usar UTF-8 válido, ter de 1 a 128 bytes, não podem conter caracteres de controle nem espaços externos e não precisam seguir UUID. A descrição tem espaços externos removidos, deve conter UTF-8 válido, não pode ser vazia e possui limite de 200 caracteres Unicode. Seu conteúdo interno, incluindo multibyte, emoji, combining marks, ZWJ e whitespace interno, não é normalizado. Uma eventual regra de produto para caracteres visualmente vazios, como U+200B, permanece planejada e não é inferida nesta etapa.

As formas aceitas são `PIX`, `DEBIT`, `CREDIT` e `CASH`. As origens aceitas são `IOS` e `WHATSAPP`; isso apenas define o vocabulário do domínio e não implementa nenhum canal. O estado inicial é `RECORDED`, com versão `1`, porque o caso de uso somente deve ser invocado depois da confirmação explícita no canal chamador.

O instante é normalizado para UTC e o timezone financeiro permanece explícito. `America/Sao_Paulo` é a baseline planejada do produto, mas UTC-3 não é hard-coded pelo domínio. `Local` é rejeitado porque depende do ambiente do processo. Ambientes futuros que executem a validação IANA deverão fornecer tzdata; isso será validado antes de containerização ou deploy, sem embutir `time/tzdata` nesta etapa.

## Aplicação e portas

`application.CreateExpense` recebe dados já revisados e confirmados pelo canal, constrói `Money` e solicita ao domínio a validação de todos os dados do canal antes de consultar ID ou relógio. A criação segura de `Expense` reutiliza a mesma fonte das invariantes, sem copiar regras na aplicação. Somente depois da validação o caso de uso obtém ID e horário por dependências explícitas, chama `ExpenseRepository.Save` exatamente uma vez e retorna a entidade após sucesso. Não existe `confirmed=true`: cada interface é responsável por não executar o caso de uso antes da confirmação; o cliente iOS da Etapa 3 cumpre preview → revisão → confirmação explícita.

`PreviewExpense` reutiliza a normalização da aplicação/domínio sem ID, Clock ou persistência. Instantes financeiros são convertidos para UTC e truncados para microssegundos em uma única função da aplicação antes de preview, fingerprint, criação persistível e resposta; o Clock passa pela mesma canonicalização para `createdAt`/`updatedAt`. `RecordExpense` representa a operação posterior à confirmação do canal, valida antes de gerar valores e delega a atomicidade à porta pequena `ExpenseCommandStore`. `ListExpensesByMonth` calcula `[start,end)` em `America/Sao_Paulo` e usa somente `ExpenseReader.ListByFinancialMonth`. As portas anteriores continuam pequenas; não há UnitOfWork, repository genérico ou dependência de pgx/HTTP na aplicação.

## Adapter PostgreSQL

`Save` inicia uma DB transaction, insere a Expense em `transactions`, insere um `EXPENSE_RECORDED` mínimo em `audit_events` e confirma somente após ambos terem sucesso. Qualquer falha provoca rollback. O evento guarda owner lógico, aggregate, versão, tipo e instante; não replica descrição, valor ou payload financeiro. Uma FK composta de `(aggregate_id, user_id)` para `transactions (id, user_id)` garante no banco que o owner do evento é o mesmo da Expense, sem incluir `aggregate_version` nessa relação.

O schema também protege invariantes essenciais com PK, FK, `UNIQUE` e `CHECK`, mas o domínio continua sendo a autoridade. Para os limites de borda, a migration enumera explicitamente o conjunto Unicode White_Space usado por `strings.TrimSpace` no Go 1.26.6: IDs e timezones com whitespace externo são rejeitados, descrições persistidas devem estar sem whitespace externo e descrições formadas somente por esse conjunto são inválidas. Whitespace interno permanece intacto e nenhuma normalização Unicode é feita. Como a tabela Unicode do Go pode evoluir em uma atualização futura da toolchain, esse conjunto SQL deverá ser revisto junto com a versão do Go; não há dependência de locale ou extensão PostgreSQL.

Instantes usam `TIMESTAMPTZ`, enquanto `financial_timezone` preserva o identificador IANA. A tabela `users` contém somente ID e timestamps para ownership/referential integrity; ela não implementa autenticação e as migrations não contêm usuários.

## API e idempotência

O adapter HTTP expõe preview, registro e consulta mensal somente quando `JARVIS_FINANCIAL_API_ENABLED=true`. O owner vem de `JARVIS_OWNER_ID` no servidor como contexto single-owner temporário; não é autenticação e nunca é aceito ou retornado pelo contrato. A API atribui `origin=IOS` e `financialTimezone=America/Sao_Paulo`. JSON é estrito e limitado, Money usa minor units inteiras e respostas financeiras desabilitam cache.

A migration 002 guarda apenas metadata técnica em `idempotency_records`: owner, operação, chave, SHA-256 da representação canônica, estado, referência da transaction e timestamps. Não replica request, descrição, amount ou response. A chave é escopada por `(owner, CREATE_EXPENSE, key)`. A mesma DB transaction reserva a chave, insere Expense, insere `EXPENSE_RECORDED`, conclui a metadata e faz commit. Replay lê a Expense original; payload diferente retorna conflito; rollback remove a reserva. Um resultado de commit indeterminado nunca deve ser tratado como sucesso e reforça a obrigação de retry com a mesma chave.

O fingerprint inclui tipo, descrição normalizada, Money, forma de pagamento, instante já canonicalizado em UTC/microssegundos, origin e timezone atribuídos pelo servidor. Duas entradas que diferem somente na precisão descartada são semanticamente equivalentes; instantes canônicos distintos geram fingerprints distintos. Exclui ID gerado, Clock e timestamps de infraestrutura. `GET` filtra owner, timezone e intervalo mensal inclusivo/exclusivo, ordenando por `occurred_at DESC, id DESC`; o índice `(user_id, occurred_at DESC, id DESC)` existe somente para essa query real.

## Decisões adiadas

Retenção de metadata idempotente, autenticação real, rate limiting distribuído e tratamento de outcomes operacionais indeterminados permanecem decisões anteriores ao uso real. Idempotência não faz parte de `Money`/`Expense`, e logs não substituem audit events nem recebem conteúdo financeiro.

Não existem autenticação, armazenamento financeiro local, IA, WhatsApp funcional, infraestrutura de nuvem ou banco de produção nesta etapa. A UI iOS implementa somente a primeira jornada e usa dados sintéticos em desenvolvimento. A API registra uma despesa já ocorrida; nunca executa Pix, pagamento, compra ou transferência.
