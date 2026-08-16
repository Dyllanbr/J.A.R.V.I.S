# Baseline de segurança

Segurança e privacidade são requisitos de arquitetura, desenvolvimento e operação, mesmo sem coleta ou persistência de dados financeiros reais.

## Controles implementados

- O servidor escuta em loopback por padrão e valida endereço, porta e timeout antes de iniciar.
- Timeouts HTTP, limite de cabeçalhos e shutdown limitado reduzem retenção indevida de recursos.
- O health check não expõe dados internos, desabilita cache e evita MIME sniffing.
- `.gitignore` exclui configurações locais, chaves, artefatos, caches e estados/planos Terraform.
- `.env.example` contém somente nomes e valores fictícios e não é carregado implicitamente.
- O scanner cobre padrões de alta confiança, imprime somente arquivo, linha e tipo, falha ao detectar material proibido e possui autoteste que verifica redação.
- Dependências npm usam lockfile; `npm ci`, `npm audit --audit-level=high` e `go mod verify` integram o quality gate.
- Actions oficiais são fixadas por SHA e documentadas na [baseline de supply chain](supply-chain.md); checkout não persiste credenciais e o CI usa apenas `contents: read`.
- O núcleo financeiro não registra logs, usa somente dados sintéticos nos testes e retorna erros categóricos sem conteúdo financeiro.
- IDs opacos do núcleo financeiro são obrigatórios, limitados a 128 bytes de UTF-8 válido e rejeitam caracteres de controle e espaços externos sem expor o valor rejeitado no erro.
- O adapter PostgreSQL usa parâmetros posicionais, `context.Context`, timeouts e DB transactions atômicas; erros públicos não incluem SQL, URL, descrição, amount ou identificadores rejeitados.
- Credenciais locais vêm exclusivamente do ambiente. O arquivo Compose fixa PostgreSQL 18.6 por digest e o CI gera credencial sintética efêmera sem secrets do GitHub.
- Não há credenciais reais, banco de produção, autenticação ou coleta operacional de usuários reais. A persistência atual contém somente dados sintéticos local/CI.
- O modo financeiro é opt-in: health-only não abre pool nem exige banco. Quando habilitado, owner, origin e timezone vêm do servidor; `userId`, origin e timezone não são aceitos do cliente.
- Requests financeiros usam DTO discriminado explícito, rejeitam propriedades desconhecidas e JSON adicional e limitam o body a 16 KiB. O decoder padrão do Go ainda aceita casing não canônico de nomes conhecidos; isso não amplia os campos permitidos, mas permanece limitação conhecida. `EXPENSE` exige payment method; `INCOME` proíbe a propriedade e persiste `payment_method IS NULL`. Responses são JSON, `no-store`, `nosniff` e nunca incluem owner, SQL, erro PostgreSQL ou body bruto.
- O cliente envia somente `categoryId` técnico opcional. O catálogo PostgreSQL read-only determina existência, label, ordenação e applicability; a aplicação valida a relação com Expense/Income e uma FK composta repete essa defesa no banco. O endpoint de catálogo não aceita mutação, e erros/logs não incluem Category recebida nem dados financeiros. A associação de Category à transaction é tratada como contexto financeiro potencialmente sensível.
- A idempotência é garantida no PostgreSQL por `(owner, operation, key)`, fingerprint SHA-256 canônico e uma única transação com o agregado e seu AuditEvent. `CREATE_EXPENSE`/`EXPENSE_RECORDED` e `CREATE_INCOME`/`INCOME_RECORDED` são restringidos ao tipo correspondente; chaves não são logadas ou devolvidas, e a tabela não replica payload financeiro.
- Leituras e replays permanecem owner-scoped. FKs compostas e constraints cross-type protegem ownership e impedem associar operação ou audit event ao tipo financeiro errado; esse owner server-side ainda não é autorização real.
- O cliente iOS usa URLSession efêmera, desabilita cache/cookies persistentes, não registra payloads e não contém owner, credencial ou autenticação fictícia. Somente o build `DEBUG` aceita o stub sintético de UI test.
- A base URL local é configurável por launch environment; Release não assume localhost nem aceita URL com credenciais. A exceção ATS `NSAllowsLocalNetworking` existe apenas no Info.plist de Debug/integration; Release não a contém e nenhum build habilita cargas arbitrárias.
- XCUITest separa explicitamente stub e API real. O modo real exige URL propagada pelo test bundle ao launch environment do app, falha fechado e tem pós-condições PostgreSQL fixture-specific para Expense e Income, impedindo falso positivo offline.
- O Simulator em loopback é o único alvo integrado atual. O backend não foi aberto à LAN para dispositivo físico sem autenticação.

## Regras obrigatórias

- Secrets devem vir de mecanismos externos ao repositório e nunca de valores versionados.
- Logs, fixtures e relatórios não podem conter dados financeiros pessoais, tokens ou credenciais.
- Entradas externas devem ser validadas no limite da aplicação.
- Dependências devem ser mínimas, justificadas, fixadas e revisadas.
- Pull requests não recebem secrets na fundação; o workflow não usa `pull_request_target` ou `continue-on-error`.
- Mudanças envolvendo dados pessoais, autenticação ou integrações exigem modelagem de ameaças e revisão independente proporcionais ao risco.
- Antes de produção deverão ser definidos papéis de banco com least privilege, TLS, backup, criptografia, retenção e gestão externa de secrets; a conta do container local/teste não é baseline de produção.
- Autenticação e rate limiting distribuído permanecem planejados; não usar o contexto single-owner temporário como controle de acesso em beta externo.
- Qualquer beta externo exige o [LGPD Readiness Gate](../privacy/privacy-by-design-checklist.md).
- Não usar a UI atual com dados reais: não há autenticação, autorização, armazenamento local seguro nem recovery de operação após restart. Descrições de Income podem revelar renda ou empregador; Category também pode revelar contexto sensível, mesmo sendo uma classificação estruturada.

O scanner local reduz risco de padrões conhecidos, mas não substitui revisão humana, secret scanning do provedor e defesa em profundidade quando a exposição do projeto crescer.
