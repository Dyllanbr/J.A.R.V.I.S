# Arquitetura

## Estado

O backend é um único processo Go organizado como monólito modular. Além da composição, configuração, ciclo de vida HTTP e health check, existe o núcleo do módulo `transactions`. Esse núcleo ainda não está conectado ao processo HTTP ou a qualquer persistência.

| Elemento | Estado |
| --- | --- |
| Monorepo | Implementado |
| Backend Go | Implementado, fundação e núcleo de transações |
| Health check HTTP | Implementado |
| Quality gate e OpenAPI 3.1 | Implementados |
| Baselines WCAG 2.2 AA e LGPD | Implementadas como documentação; conformidade não alegada |
| `transactions`: Money, Expense e CreateExpense | Implementado; revisão independente pendente |
| PostgreSQL | Planejado, sem driver ou instância |
| Aplicativo SwiftUI/iOS | Planejado, sem projeto Xcode |
| Terraform/nuvem | Planejado, sem configuração |

## Direção de dependências

A composição acontece em `cmd/api` e `internal/app`. O adaptador HTTP fica em `internal/platform/httpserver`. A direção adotada para módulos de negócio é:

```text
transportes e persistência -> casos de uso -> domínio
              composição conecta dependências explícitas
```

Domínio e casos de uso não poderão importar HTTP, SQL, SDKs de IA ou integrações. Interfaces deverão nascer de necessidades reais dos casos de uso; não serão criadas antecipadamente. Handlers traduzirão entrada e saída e não conterão regras de negócio nem SQL.

## Limites dos módulos futuros

Cada módulo é um pacote coeso em `backend/internal/modules/<nome>`, com vocabulário e responsabilidades claros. `transactions/domain` não conhece aplicação ou infraestrutura; `transactions/application` depende do domínio e declara as portas mínimas que consome. Comunicação futura entre módulos ocorrerá por APIs internas explícitas. Compartilhamento será aceito apenas quando houver uma responsabilidade estável e nomeável; um pacote genérico `utils` não faz parte da arquitetura.

## Configuração e execução

A configuração vem do ambiente, é validada no início e não carrega `.env` implicitamente. A porta operacional deve estar entre 1 e 65535; porta dinâmica é restrita a harnesses controlados de teste. O endereço padrão usa apenas loopback. O servidor estabelece timeouts de cabeçalho, leitura, escrita e conexão ociosa, limite de cabeçalhos e desligamento gracioso limitado a 30 segundos. O primeiro `SIGINT` ou `SIGTERM` inicia o shutdown; um segundo sinal volta ao comportamento padrão e pode forçar o encerramento.

Startup registra apenas o endereço efetivamente ligado, shutdown registra o término e erros são reportados sem conteúdo de requisição ou configuração sensível. Logs não devem receber dados pessoais, financeiros ou secrets. O processo usa configuração e caminhos relativos ao repositório; isso preserva portabilidade e futura containerização, sem introduzir container nesta fase.

O domínio valida nomes de timezone pela base IANA do ambiente. `America/Sao_Paulo` é a baseline financeira planejada e UTC-3 não pode ser hard-coded. A disponibilidade de tzdata será um requisito operacional a verificar antes de containerização ou deploy; a Etapa 1 não embute `time/tzdata` sem necessidade técnica comprovada.

## Evolução

Cada nova capacidade deve chegar em uma mudança vertical pequena: contrato, domínio, caso de uso, adaptadores e testes proporcionais ao risco. Tecnologias externas serão adicionadas somente quando houver um caso concreto e uma decisão registrada. Mudanças com dados pessoais exigem o [LGPD Readiness Gate](../privacy/privacy-by-design-checklist.md) quando houver beta externo; interfaces seguem a [baseline WCAG 2.2 AA](../accessibility/baseline.md).
