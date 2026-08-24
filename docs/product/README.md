# Documentação de produto

- Maturidade documental: **Proposed**
- Estado das capacidades: **misto** — consulte cada documento e capacidade

Este diretório reúne a visão, os princípios e a direção estratégica do J.A.R.V.I.S. Ele não substitui especificações técnicas, controles especializados, evidências de qualidade nem o backlog operacional.

O [Product Book](product-book.md) é a fonte estratégica central do produto. Ele descreve o que o J.A.R.V.I.S. pretende ser, seus limites, sua relação com o usuário, as capacidades já entregues e a visão futura sem transformar intenção em alegação de implementação.

## Fontes de verdade

| Assunto | Fonte |
| --- | --- |
| Visão, posicionamento e princípios de produto | [Product Book](product-book.md) |
| Decisões arquiteturais aceitas | [ADRs](../adr/README.md) |
| Arquitetura e estado técnico | [Visão de arquitetura](../architecture/overview.md) |
| Segurança | [Baseline de segurança](../security/baseline.md) |
| Privacidade e LGPD | [Documentação de privacidade](../privacy/README.md) |
| Acessibilidade | [Baseline de acessibilidade](../accessibility/baseline.md) |
| Estratégia e evidências de testes | [Estratégia de QA](../qa/testing-strategy.md) |
| Performance e operabilidade | [Baseline de performance](../performance/baseline.md) |
| Critérios de entrega e verificação | [Definition of Done](../quality/definition-of-done.md) |
| Backlog, prioridade e andamento operacional | GitHub Project |

O GitHub Project continua sendo a fonte de verdade operacional do backlog. Os documentos de produto podem registrar direção, horizontes, dependências e decisões, mas não devem copiar todas as Issues nem manter uma segunda lista operacional concorrente.

## Dois eixos de status

Status documental e estado de entrega respondem a perguntas diferentes e não devem ser combinados em um único rótulo.

### Maturidade documental

- **Proposed:** rascunho estratégico em discussão; ainda pode mudar.
- **Approved:** conteúdo revisado e aceito como direção vigente.
- **Published:** conteúdo aprovado e disponibilizado ao público ou audiência pretendida.
- **Deprecated:** documento mantido apenas para histórico e substituído por outra referência.

### Estado de entrega de capacidades

- **Planned:** capacidade decidida ou descrita, mas ainda não implementada.
- **Implemented:** código, contrato ou documentação aplicável existe, sem implicar verificação completa.
- **Verified:** implementação passou pelos critérios e quality gates aplicáveis e possui evidência correspondente.

**Approved não significa Implemented. Published não significa Verified.** Uma capacidade futura pode estar descrita em um documento **Approved** e continuar **Planned**. O estado **Verified** exige evidência compatível com os quality gates, a [Definition of Done](../quality/definition-of-done.md) e revisões proporcionais ao risco.

## Visão, planejamento, implementação e evidência

- **Visão:** define propósito, princípios, limites e resultados desejados; pertence ao Product Book.
- **Planejamento:** transforma a direção em horizontes e prioridades; o [roadmap estratégico](roadmap.md) organiza a sequência, enquanto o GitHub Project continua operacional.
- **Implementação:** existe no código, contratos e documentação técnica; sem evidência suficiente de verificação, a capacidade permanece **Implemented**.
- **Evidência:** testes, auditorias e gates sustentam o estado **Verified** nas fontes especializadas.

Uma declaração de visão não comprova planejamento detalhado. Um item planejado não comprova implementação. Uma implementação não se torna verificada sem evidência.

## Índice

### Documentos atuais

- `README.md` — índice, governança e fontes de verdade; **Proposed**.
- [Product Book](product-book.md) — visão estratégica central; **Proposed**.
- [Princípios do assessor](advisor-principles.md) — comportamento, verdade, tom, proatividade e silêncio; **Proposed**, capacidade **Planned**.
- [Personalização](personalization.md) — modelo financeiro individual, memória, proveniência e controles; **Proposed**, capacidade **Planned**.
- [Análise competitiva](competitive-analysis.md) — fotografia estratégica datada, padrões de mercado e hipóteses de diferenciação; **Proposed**.
- [Princípios de design](design-principles.md) — direção visual, experiência, acessibilidade e critérios para um futuro design system; **Proposed**, capacidade **Planned**.
- [Roadmap estratégico](roadmap.md) — sequência de evolução, resultados, dependências e gates sem duplicar o backlog; **Proposed**, capacidades em estado **misto**.

Os Incrementos 1 — Despesas, 2 — Receitas, 3A — Categorias e filtros do histórico e 3B — Recorrências confirmadas e assinaturas estão **Verified**, com implementação mergeada e evidências compatíveis com os quality gates aplicáveis. O Incremento 3C — Detecção e sugestão de recorrências permanece **Planned** e é acompanhado pela Issue #70. O Product Book separa essas capacidades verificadas da visão futura.

## Regra de manutenção

Documentos de produto devem apontar para as fontes especializadas em vez de copiar requisitos técnicos inteiros. Capacidades futuras precisam aparecer como **Planned**; fatos atuais devem corresponder ao que o repositório e suas evidências comprovam. Mudanças de arquitetura, segurança, privacidade, acessibilidade, QA ou performance continuam sendo registradas e verificadas em seus documentos próprios.
