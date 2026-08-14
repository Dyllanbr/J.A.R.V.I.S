# Arquitetura da fundação

## Estado

O backend é um único processo Go, estruturado para evoluir como monólito modular. A implementação atual contém somente composição, configuração, ciclo de vida HTTP e health check; ainda não há módulos de negócio.

| Elemento | Estado |
| --- | --- |
| Monorepo | Implementado |
| Backend Go | Implementado, somente fundação |
| Health check HTTP | Implementado |
| Quality gate e OpenAPI 3.1 | Implementados |
| Baselines WCAG 2.2 AA e LGPD | Implementadas como documentação; conformidade não alegada |
| Módulos de domínio | Planejados, nenhum criado |
| PostgreSQL | Planejado, sem driver ou instância |
| Aplicativo SwiftUI/iOS | Planejado, sem projeto Xcode |
| Terraform/nuvem | Planejado, sem configuração |

## Direção de dependências

A composição acontece em `cmd/api` e `internal/app`. O adaptador HTTP fica em `internal/platform/httpserver`. Quando módulos de negócio surgirem, a direção pretendida será:

```text
transportes e persistência -> casos de uso -> domínio
              composição conecta dependências explícitas
```

Domínio e casos de uso não poderão importar HTTP, SQL, SDKs de IA ou integrações. Interfaces deverão nascer de necessidades reais dos casos de uso; não serão criadas antecipadamente. Handlers traduzirão entrada e saída e não conterão regras de negócio nem SQL.

## Limites dos módulos futuros

Cada módulo será um pacote coeso em `backend/internal/modules/<nome>`, com vocabulário e responsabilidades claros. Comunicação entre módulos ocorrerá por APIs internas explícitas. Compartilhamento será aceito apenas quando houver uma responsabilidade estável e nomeável; um pacote genérico `utils` não faz parte da arquitetura.

## Configuração e execução

A configuração vem do ambiente, é validada no início e não carrega `.env` implicitamente. A porta operacional deve estar entre 1 e 65535; porta dinâmica é restrita a harnesses controlados de teste. O endereço padrão usa apenas loopback. O servidor estabelece timeouts de cabeçalho, leitura, escrita e conexão ociosa, limite de cabeçalhos e desligamento gracioso limitado a 30 segundos. O primeiro `SIGINT` ou `SIGTERM` inicia o shutdown; um segundo sinal volta ao comportamento padrão e pode forçar o encerramento.

Startup registra apenas o endereço efetivamente ligado, shutdown registra o término e erros são reportados sem conteúdo de requisição ou configuração sensível. Logs não devem receber dados pessoais, financeiros ou secrets. O processo usa configuração e caminhos relativos ao repositório; isso preserva portabilidade e futura containerização, sem introduzir container nesta fase.

## Evolução

Cada nova capacidade deve chegar em uma mudança vertical pequena: contrato, domínio, caso de uso, adaptadores e testes proporcionais ao risco. Tecnologias externas serão adicionadas somente quando houver um caso concreto e uma decisão registrada. Mudanças com dados pessoais exigem o [LGPD Readiness Gate](../privacy/privacy-by-design-checklist.md) quando houver beta externo; interfaces seguem a [baseline WCAG 2.2 AA](../accessibility/baseline.md).
