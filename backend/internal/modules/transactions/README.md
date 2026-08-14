# Módulo de transações

## Estado atual

| Item | Estado | Evidência candidata |
| --- | --- | --- |
| `Money` em minor units inteiras com BRL | VERIFICADO | Etapa 1 mergeada, testes unitários/fuzz e revisão independente |
| Entidade `Expense` e invariantes | VERIFICADO | Etapa 1 mergeada, testes unitários/fuzz e revisão independente |
| Caso de uso `CreateExpense` | VERIFICADO | Etapa 1 mergeada, testes determinísticos e revisão independente |
| Persistência PostgreSQL | IMPLEMENTADO | adapter e integração real aguardam revisão independente da Etapa 2A |
| Migrations | IMPLEMENTADO | UP/DOWN/reaplicação aguardam revisão independente da Etapa 2A |
| Idempotência | PLANEJADO | não implementada |
| `AuditEvent` transacional | IMPLEMENTADO | evento mínimo atômico aguarda revisão independente da Etapa 2A |
| API, iOS e demais canais | PLANEJADO | não implementados |

O estado **VERIFICADO** depende de quality gate e revisão independente conforme a Definition of Done; não é atribuído autonomamente por esta implementação.

## Domínio

`domain` não importa HTTP, JSON, SQL, banco de dados ou integrações. `Money` armazena somente um `int64` de minor units. Como BRL é a única moeda desta etapa, `Currency()` retorna BRL e o zero value de `Money` representa validamente R$ 0,00 BRL; `NewMoney` ainda rejeita qualquer moeda solicitada diferente de BRL. A igualdade compara os minor units, pois toda instância possui a mesma moeda implícita. R$ 42,50 é representado por `4250`. `Money` aceita valores assinados e zero como valor monetário genérico; uma `Expense` exige valor estritamente positivo.

Uma despesa possui identificadores, tipo fixo `EXPENSE`, descrição, valor, forma de pagamento, instante de ocorrência, timezone financeiro IANA, origem, status, versão e timestamps. ID e UserID são identificadores opacos: devem usar UTF-8 válido, ter de 1 a 128 bytes, não podem conter caracteres de controle nem espaços externos e não precisam seguir UUID. A descrição tem espaços externos removidos, deve conter UTF-8 válido, não pode ser vazia e possui limite de 200 caracteres Unicode. Seu conteúdo interno, incluindo multibyte, emoji, combining marks, ZWJ e whitespace interno, não é normalizado. Uma eventual regra de produto para caracteres visualmente vazios, como U+200B, permanece planejada e não é inferida nesta etapa.

As formas aceitas são `PIX`, `DEBIT`, `CREDIT` e `CASH`. As origens aceitas são `IOS` e `WHATSAPP`; isso apenas define o vocabulário do domínio e não implementa nenhum canal. O estado inicial é `RECORDED`, com versão `1`, porque o caso de uso somente deve ser invocado depois da confirmação explícita no canal chamador.

O instante é normalizado para UTC e o timezone financeiro permanece explícito. `America/Sao_Paulo` é a baseline planejada do produto, mas UTC-3 não é hard-coded pelo domínio. `Local` é rejeitado porque depende do ambiente do processo. Ambientes futuros que executem a validação IANA deverão fornecer tzdata; isso será validado antes de containerização ou deploy, sem embutir `time/tzdata` nesta etapa.

## Aplicação e portas

`application.CreateExpense` recebe dados já revisados e confirmados pelo canal, constrói `Money` e solicita ao domínio a validação de todos os dados do canal antes de consultar ID ou relógio. A criação segura de `Expense` reutiliza a mesma fonte das invariantes, sem copiar regras na aplicação. Somente depois da validação o caso de uso obtém ID e horário por dependências explícitas, chama `ExpenseRepository.Save` exatamente uma vez e retorna a entidade após sucesso. Não existe `confirmed=true`: interfaces futuras são responsáveis por não executar o caso de uso antes da confirmação.

As únicas portas são `ExpenseRepository`, `ExpenseIDGenerator` e `Clock`. A interface existente não foi ampliada. `adapters/postgres` implementa `ExpenseRepository` com SQL explícito e parametrizado; `application` e `domain` não importam pgx, migrations ou plataforma.

## Adapter PostgreSQL

`Save` inicia uma DB transaction, insere a Expense em `transactions`, insere um `EXPENSE_RECORDED` mínimo em `audit_events` e confirma somente após ambos terem sucesso. Qualquer falha provoca rollback. O evento guarda owner lógico, aggregate, versão, tipo e instante; não replica descrição, valor ou payload financeiro. Uma FK composta de `(aggregate_id, user_id)` para `transactions (id, user_id)` garante no banco que o owner do evento é o mesmo da Expense, sem incluir `aggregate_version` nessa relação.

O schema também protege invariantes essenciais com PK, FK, `UNIQUE` e `CHECK`, mas o domínio continua sendo a autoridade. Para os limites de borda, a migration enumera explicitamente o conjunto Unicode White_Space usado por `strings.TrimSpace` no Go 1.26.6: IDs e timezones com whitespace externo são rejeitados, descrições persistidas devem estar sem whitespace externo e descrições formadas somente por esse conjunto são inválidas. Whitespace interno permanece intacto e nenhuma normalização Unicode é feita. Como a tabela Unicode do Go pode evoluir em uma atualização futura da toolchain, esse conjunto SQL deverá ser revisto junto com a versão do Go; não há dependência de locale ou extensão PostgreSQL.

Instantes usam `TIMESTAMPTZ`, enquanto `financial_timezone` preserva o identificador IANA. A tabela `users` contém somente ID e timestamps para ownership/referential integrity; ela não implementa autenticação e as migrations não contêm usuários.

## Decisões adiadas

Idempotência pertence à futura command/API boundary da Etapa 2B. Uma chave e seu armazenamento ainda não foram definidos e não fazem parte de `Money`, `Expense` ou do adapter desta etapa. Logs não substituem audit events e não devem receber conteúdo financeiro.

Não existem endpoint financeiro, UI, autenticação, IA, WhatsApp funcional, infraestrutura de nuvem ou banco de produção nesta etapa.
