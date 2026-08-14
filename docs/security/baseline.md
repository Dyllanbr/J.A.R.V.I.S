# Baseline de segurança

Segurança e privacidade são requisitos de arquitetura, desenvolvimento e operação, mesmo antes de existir dado financeiro.

## Controles implementados

- O servidor escuta em loopback por padrão e valida endereço, porta e timeout antes de iniciar.
- Timeouts HTTP, limite de cabeçalhos e shutdown limitado reduzem retenção indevida de recursos.
- O health check não expõe dados internos, desabilita cache e evita MIME sniffing.
- `.gitignore` exclui configurações locais, chaves, artefatos, caches e estados/planos Terraform.
- `.env.example` contém somente nomes e valores fictícios e não é carregado implicitamente.
- O scanner cobre padrões de alta confiança, imprime somente arquivo, linha e tipo, falha ao detectar material proibido e possui autoteste que verifica redação.
- Dependências npm usam lockfile; `npm ci`, `npm audit --audit-level=high` e `go mod verify` integram o quality gate.
- Actions oficiais são fixadas por SHA e documentadas na [baseline de supply chain](supply-chain.md); checkout não persiste credenciais e o CI usa apenas `contents: read`.
- O projeto não possui credenciais, banco, SDKs externos nem coleta de dados.

## Regras obrigatórias

- Secrets devem vir de mecanismos externos ao repositório e nunca de valores versionados.
- Logs, fixtures e relatórios não podem conter dados financeiros pessoais, tokens ou credenciais.
- Entradas externas devem ser validadas no limite da aplicação.
- Dependências devem ser mínimas, justificadas, fixadas e revisadas.
- Pull requests não recebem secrets na fundação; o workflow não usa `pull_request_target` ou `continue-on-error`.
- Mudanças envolvendo dados pessoais, autenticação ou integrações exigem modelagem de ameaças e revisão independente proporcionais ao risco.
- Qualquer beta externo exige o [LGPD Readiness Gate](../privacy/privacy-by-design-checklist.md).

O scanner local reduz risco de padrões conhecidos, mas não substitui revisão humana, secret scanning do provedor e defesa em profundidade quando a exposição do projeto crescer.
