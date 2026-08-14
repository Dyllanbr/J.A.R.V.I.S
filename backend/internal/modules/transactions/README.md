# Módulo de transações

## Estado da Etapa 1

| Item | Estado | Evidência candidata |
| --- | --- | --- |
| `Money` em minor units inteiras com BRL | IMPLEMENTADO | testes unitários e fuzz seeds em `domain` |
| Entidade `Expense` e invariantes | IMPLEMENTADO | testes unitários e fuzz seeds de descrição em `domain` |
| Caso de uso `CreateExpense` | IMPLEMENTADO | testes unitários determinísticos em `application` |
| Persistência PostgreSQL | PLANEJADO | não implementada |
| Idempotência | PLANEJADO | não implementada |
| `AuditEvent` transacional | PLANEJADO | não implementado |
| API, iOS e demais canais | PLANEJADO | não implementados |

O estado **VERIFICADO** depende de quality gate e revisão independente conforme a Definition of Done; não é atribuído autonomamente por esta implementação.

## Domínio

`domain` não importa HTTP, JSON, SQL, banco de dados ou integrações. `Money` armazena somente um `int64` de minor units. Como BRL é a única moeda desta etapa, `Currency()` retorna BRL e o zero value de `Money` representa validamente R$ 0,00 BRL; `NewMoney` ainda rejeita qualquer moeda solicitada diferente de BRL. A igualdade compara os minor units, pois toda instância possui a mesma moeda implícita. R$ 42,50 é representado por `4250`. `Money` aceita valores assinados e zero como valor monetário genérico; uma `Expense` exige valor estritamente positivo.

Uma despesa possui identificadores, tipo fixo `EXPENSE`, descrição, valor, forma de pagamento, instante de ocorrência, timezone financeiro IANA, origem, status, versão e timestamps. ID e UserID são identificadores opacos: devem usar UTF-8 válido, ter de 1 a 128 bytes, não podem conter caracteres de controle nem espaços externos e não precisam seguir UUID. A descrição tem espaços externos removidos, deve conter UTF-8 válido, não pode ser vazia e possui limite de 200 caracteres Unicode. Seu conteúdo interno, incluindo multibyte, emoji, combining marks, ZWJ e whitespace interno, não é normalizado. Uma eventual regra de produto para caracteres visualmente vazios, como U+200B, permanece planejada e não é inferida nesta etapa.

As formas aceitas são `PIX`, `DEBIT`, `CREDIT` e `CASH`. As origens aceitas são `IOS` e `WHATSAPP`; isso apenas define o vocabulário do domínio e não implementa nenhum canal. O estado inicial é `RECORDED`, com versão `1`, porque o caso de uso somente deve ser invocado depois da confirmação explícita no canal chamador.

O instante é normalizado para UTC e o timezone financeiro permanece explícito. `America/Sao_Paulo` é a baseline planejada do produto, mas UTC-3 não é hard-coded pelo domínio. `Local` é rejeitado porque depende do ambiente do processo. Ambientes futuros que executem a validação IANA deverão fornecer tzdata; isso será validado antes de containerização ou deploy, sem embutir `time/tzdata` nesta etapa.

## Aplicação e portas

`application.CreateExpense` recebe dados já revisados e confirmados pelo canal, constrói `Money` e solicita ao domínio a validação de todos os dados do canal antes de consultar ID ou relógio. A criação segura de `Expense` reutiliza a mesma fonte das invariantes, sem copiar regras na aplicação. Somente depois da validação o caso de uso obtém ID e horário por dependências explícitas, chama `ExpenseRepository.Save` exatamente uma vez e retorna a entidade após sucesso. Não existe `confirmed=true`: interfaces futuras são responsáveis por não executar o caso de uso antes da confirmação.

As únicas portas são `ExpenseRepository`, `ExpenseIDGenerator` e `Clock`. Os fakes existem apenas nos testes; não há adaptador de persistência de produção.

## Decisões adiadas

Idempotência pertence à camada de aplicação e ao limite do canal/persistência. Uma chave e seu armazenamento serão definidos quando esse limite existir; ela não faz parte de `Money` ou `Expense`. `AuditEvent` deverá ser persistido atomicamente com a despesa quando houver uma transação real. Logs não substituem auditoria e não devem receber conteúdo financeiro.

Não existem nesta etapa SQL, migrations, PostgreSQL, endpoint financeiro, UI, autenticação, IA, WhatsApp funcional ou infraestrutura de nuvem.
