# Roadmap estratégico do produto

- Maturidade documental: **Proposed**
- Estado das capacidades: **misto** — cada etapa indica seu próprio estado

Este documento organiza a direção estratégica de evolução do J.A.R.V.I.S. Ele descreve resultados desejados, sequência predominante, dependências e gates de confiança. Não é cronograma, especificação técnica nem promessa de entrega.

O [Product Book](product-book.md) permanece como fonte central da visão e dos limites do produto. O roadmap conecta essa visão a uma progressão de capacidades sem reproduzir o conteúdo das fontes especializadas.

## Como ler os estados

- **Verified:** capacidade implementada com evidência compatível com os quality gates aplicáveis.
- **Implemented:** capacidade existente, mas ainda sem evidência suficiente para ser classificada como verificada.
- **Planned:** direção ou capacidade pretendida, ainda não implementada.
- **Future:** horizonte estratégico, e não um estado adicional de entrega. Uma capacidade neste horizonte permanece **Planned**, salvo indicação explícita em contrário.

O status do documento não altera o estado das capacidades. Um roadmap **Proposed** pode registrar uma capacidade **Verified** e várias capacidades **Planned**.

## Roadmap não é backlog

Este roadmap registra:

- direção e resultados estratégicos;
- sequência predominante;
- dependências entre capacidades;
- gates necessários antes de ampliar o uso.

O GitHub Project continua sendo a fonte de verdade operacional para:

- Issues e sua decomposição;
- prioridades vigentes;
- responsáveis;
- estado de execução;
- progresso e decisões operacionais.

O roadmap não copia todas as Issues e não mantém um backlog paralelo. Alterações de prioridade ou execução pertencem ao Project; mudanças relevantes de direção pertencem a este documento e às fontes estratégicas relacionadas.

## Lógica de evolução

A direção predominante é evoluir de:

**registrar → organizar → compreender → prever → proteger → simular → aconselhar → personalizar**

Essa progressão não transforma o J.A.R.V.I.S. em banco nem em executor de transações. O produto organiza, analisa, explica e apoia decisões; o usuário continua decidindo.

| Etapa | Estado | Resultado estratégico predominante |
| --- | --- | --- |
| Fundação | **Verified** | Base técnica e de qualidade para evolução segura |
| Incremento 1 — Despesas | **Verified** | Primeiro ciclo financeiro completo |
| Incremento 2 — Receitas | **Verified** | Registro e consulta de entradas e saídas com evidência final de qualidade |
| Incremento 3A — Categorias e filtros do histórico | **Verified** | Organização opcional por categorias do sistema, auditada e verificada |
| Incremento 3B — Recorrências confirmadas e assinaturas | **Verified** | Compromissos recorrentes manuais, separados de Expense, Income e Category |
| Incremento 3C — Detecção e sugestão de recorrências | **Planned** | Observar padrões, sugerir recorrências e exigir confirmação antes de promovê-las |
| Incremento 4 — Cartões, parcelas e compromissos futuros | **Planned** | Compreensão do dinheiro já comprometido |
| Incremento 5 — Orçamento e Disponível Seguro | **Planned** | Resposta explicável sobre quanto pode ser gasto |
| Incremento 6 — Metas e “Posso comprar?” | **Planned** | Apoio estruturado a decisões financeiras |
| Incremento 7 — Identidade, autenticação e Trust Center | **Planned** | Base de confiança para uso pessoal real e multiusuário |
| Incremento 8 — WhatsApp | **Planned** | Conveniência com continuidade e autoridade preservada no app |
| Incremento 9 — Assessor com IA | **Planned** | Explicação consultiva sobre dados e cálculos estruturados |
| Incremento 10 — Hardening para uso real | **Planned** | Preparação proporcional para ampliar o uso |

## Fundação

**Estado: Verified.**

A Fundação estabeleceu o ambiente no qual as capacidades financeiras podem evoluir com segurança e evidência. Inclui, em nível resumido:

- monorepo e tooling;
- quality gates e CI;
- configuração e backend mínimo;
- arquitetura e OpenAPI;
- documentação-base;
- baselines iniciais de segurança, privacidade e QA.

As fontes técnicas permanecem na [visão de arquitetura](../architecture/overview.md), nos [ADRs](../adr/README.md), na [baseline de segurança](../security/baseline.md) e na [estratégia de QA](../qa/testing-strategy.md).

## Incremento 1 — Despesas

**Estado: Verified.**

O primeiro incremento estabeleceu o ciclo financeiro completo:

**entrada → validação → confirmação → persistência → auditoria → consulta**

As capacidades comprovadas incluem, resumidamente:

- domínio de despesas;
- persistência PostgreSQL e auditoria;
- preview e confirmação explícita;
- idempotência;
- histórico mensal;
- primeiro fluxo nativo iOS;
- integração real iOS → backend → PostgreSQL;
- testes e quality gates correspondentes.

Esse incremento é a base validada sobre a qual as próximas capacidades financeiras serão construídas.

## Incremento 2 — Receitas

**Estado: Verified.**

O incremento implementa a base para o sistema representar tanto entrada quanto saída de dinheiro:

- `Income` como agregado de escrita separado de `Expense`;
- preview, confirmação explícita e registro idempotente;
- persistência/auditoria atômicas com `CREATE_INCOME` e `INCOME_RECORDED`;
- API discriminada e histórico mensal misto de Expense/Income;
- fluxo iOS completo e integração real Simulator → app → API → PostgreSQL.

O histórico não inclui totais, saldo, orçamento ou Disponível Seguro. Essas entradas criam base para análises futuras sem antecipar cálculos ou aconselhamento. O incremento passou pela auditoria global independente e pelos quality gates aplicáveis, portanto a capacidade está **Verified**.

## Incremento 3A — Categorias e filtros do histórico

**Estado: Verified.**

O objetivo é organizar melhor registros e facilitar sua interpretação sem antecipar análise financeira. A entrega inclui:

- Category opcional para Expense e Income;
- catálogo read-only de categorias do sistema;
- “Sem categoria” distinto das categorias reais “Outros”;
- descoberta do catálogo pela API e `categoryId` opcional nos fluxos financeiros;
- picker no registro, Category congelada na revisão e labels no histórico;
- filtros client-side por tipo e Category, preservando a ordem mensal do backend.

Não existem categorias customizadas, CRUD, reclassificação, search, totais, saldo, agrupamento, recorrência ou categorização automática. A auditoria final independente foi concluída sem findings novos P0, P1 ou P2; a capacidade está **Verified**. A categoria `expense.subscriptions` permanece somente classificação e não representa uma recorrência.

## Incremento 3B — Recorrências confirmadas e assinaturas

**Estado: Verified.**

O Incremento 3B implementa `Recurrence` como agregado próprio para representar compromissos financeiros esperados, separado de `Expense`, `Income` e da categoria `expense.subscriptions`.

A entrega verificada inclui:

- criação manual de recorrências de despesa;
- fluxo Preview → Review → Confirm;
- periodicidade mensal;
- valor esperado positivo em BRL minor units;
- data civil de início com preservação do anchor day;
- lifecycle `ACTIVE → CANCELLED`, sem reativação nesta versão;
- cancelamento explícito;
- idempotência própria para criação e cancelamento;
- replay histórico preservado;
- persistência e auditoria PostgreSQL dedicadas pela migration 005;
- API REST/OpenAPI própria;
- terceira área nativa no iOS para Recorrências;
- testes unitários, integração PostgreSQL, Playwright, XCTest, XCUITest e E2E real Simulator → API → PostgreSQL.

Uma `Recurrence` representa uma expectativa, não um fato financeiro ocorrido. Ela nunca cria `Expense` ou `Income` automaticamente, não executa pagamentos e não movimenta dinheiro.

Detecção automática, confiança e sugestão de possíveis recorrências não fazem parte deste incremento. Essas capacidades foram separadas para o Incremento 3C.

## Incremento 3C — Detecção e sugestão de recorrências

**Estado: Planned.**

O objetivo é detectar padrões que possam representar compromissos recorrentes sem transformar uma hipótese em obrigação futura automaticamente.

O fluxo conceitual permanece:

**Observar → Inferir → Sugerir → Confirmar**

Uma possível recorrência detectada não é estado do aggregate `Recurrence`. A detecção deve permanecer separada do lifecycle `ACTIVE → CANCELLED`, e somente a confirmação explícita do usuário poderá iniciar a criação de uma recorrência confirmada.

A evolução é acompanhada operacionalmente pela Issue #70 e poderá futuramente alimentar projeções, orçamento, Disponível Seguro, alertas, Personal Financial Model e nudges contextuais. Essas capacidades futuras permanecem **Planned**.

## Incremento 4 — Cartões, parcelas e compromissos futuros

**Estado: Planned.**

O objetivo é compreender dinheiro já comprometido. A direção inclui:

- cartões e faturas;
- parcelas;
- vencimentos;
- compromissos financeiros futuros;
- visão dos períodos seguintes.

**Saldo disponível não é automaticamente dinheiro seguro para gastar.** Conhecer obrigações futuras prepara o sistema para calcular um Disponível Seguro com mais contexto.

## Incremento 5 — Orçamento e Disponível Seguro

**Estado: Planned.**

O objetivo é transformar dados financeiros em uma resposta mais útil para a pergunta:

> Quanto eu realmente posso gastar?

O Disponível Seguro deverá ser:

- determinístico;
- explicável e decomponível;
- transparente quanto a hipóteses e dados ausentes;
- sensível aos compromissos conhecidos;
- apresentado sem promessa de garantia absoluta.

Este roadmap não define sua fórmula final. O conceito e seus limites estão descritos no [Product Book](product-book.md), e a experiência futura deve seguir os [princípios de design](design-principles.md).

## Incremento 6 — Metas e “Posso comprar?”

**Estado: Planned.**

O objetivo é evoluir da organização para o apoio estruturado à decisão. A direção inclui:

- metas financeiras e valores protegidos;
- simulações de impacto futuro;
- comparação de alternativas;
- simulador “Posso comprar?”.

A simulação não executa a compra nem movimenta dinheiro.

A separação conceitual permanece:

> Financial Engine calcula.
>
> Personal Financial Model contextualiza.
>
> IA explica.
>
> Usuário decide.

## Incremento 7 — Identidade, autenticação e Trust Center

**Estado: Planned.**

O objetivo é criar a base de confiança necessária para uso pessoal real e, posteriormente, multiusuário. A direção conceitual inclui:

- autenticação e autorização;
- passkeys, Face ID e PIN J.A.R.V.I.S.;
- recovery;
- cache local criptografado e sincronização segura;
- Trust Center;
- controles de privacidade;
- controles de memória e personalização.

Esta seção não é especificação de segurança. Requisitos, riscos e decisões devem permanecer nas fontes próprias de [segurança](../security/) e [privacidade](../privacy/).

## Incremento 8 — WhatsApp

**Estado: Planned.**

O objetivo é adicionar conveniência sem retirar a autoridade do app.

**WhatsApp = conveniência. App = autoridade para ações e decisões sensíveis.**

A direção conceitual inclui:

- entrada rápida e conversa;
- confirmação antes de registros financeiros;
- vínculo seguro com o iPhone;
- continuidade de contexto entre canais.

Detalhes de webhook, provedor ou infraestrutura não pertencem a este roadmap.

## Incremento 9 — Assessor com IA

**Estado: Planned.**

O objetivo é evoluir de:

> O que aconteceu com meu dinheiro?

para:

> O que eu deveria fazer?

O assessor deve se apoiar em capacidades financeiras estruturadas, não substituir suas regras. A direção inclui:

- interpretação de intenção e perguntas financeiras;
- explicações contextualizadas;
- ferramentas estruturadas;
- uso do Financial Engine;
- advisor behavior coerente;
- contexto financeiro individual;
- isolamento entre a IA e o banco de dados.

> “IA para conversar. Regras para calcular. Você para decidir.”

A IA não deve possuir acesso irrestrito ao banco de dados. Os princípios de comportamento permanecem em [Princípios do assessor](advisor-principles.md), e o contexto individual futuro em [Personalização](personalization.md).

## Incremento 10 — Hardening para uso real

**Estado: Planned.**

O objetivo é preparar o sistema, de forma proporcional ao risco, para sair de um ambiente pessoal ou controlado em direção a um uso real mais amplo. As áreas incluem:

- segurança e observabilidade;
- backup, restauração e testes de disaster recovery;
- infraestrutura cloud quando justificada;
- preparação para LGPD;
- performance;
- verificação manual de acessibilidade;
- testes em dispositivos físicos;
- security release gate.

Hardening não fica inteiramente adiado para este incremento: controles proporcionais continuam evoluindo ao longo de todas as etapas. O Incremento 10 concentra os gates necessários para ampliar o uso.

### Security assurance e DevSecOps progressivos

As ferramentas abaixo representam uma direção avaliável, não escolhas imutáveis nem capacidades já implementadas.

1. **Fase 1:** avaliar Gitleaks ou ferramenta equivalente para secrets, `govulncheck` e gosec ou equivalente para Go.
2. **Fase 2:** executar uma prova de conceito com OpenGrep ou SAST equivalente e avaliar regras específicas do J.A.R.V.I.S.
3. **Fase 3:** introduzir OWASP ZAP Baseline ou análise passiva equivalente e DAST da API em ambiente descartável, orientado pelo contrato OpenAPI.
4. **Fase 4:** acrescentar DAST autenticado quando autenticação existir e o ambiente de teste for adequado.
5. **Fase 5:** consolidar security release gate, threat modeling, revisão proporcional de ASVS/MASVS e pentest antes de um beta externo relevante.

Guardrails dessa trilha:

- scanners não substituem revisão humana ou arquitetural;
- DAST não substitui pentest;
- cada ferramenta precisa de política de severidade e de tratamento explícito de falsos positivos;
- active scan não deve atingir produção por padrão;
- adoção depende de evidência de valor, manutenção e compatibilidade com os gates existentes.

### Auditoria independente por IA

A auditoria independente por IA é uma prática complementar de qualidade e security assurance, não substituta de especialistas. O fluxo desejado é:

**implementação → testes → análise automática → auditoria independente read-only por IA → correções → reauditoria → PR/CI**

As auditorias devem ter escopo específico e buscar evidências. Classes futuras de revisão podem incluir:

- IDOR, autorização e isolamento entre usuários;
- trust boundaries;
- SQL injection, command injection, path traversal e SSRF;
- mass assignment, validação de entrada e limites de payload;
- idempotência, race conditions e replay;
- exposição de segredos, logging sensível e tratamento de erros;
- autenticação, recovery e webhooks;
- acesso da IA a dados;
- proveniência e reconciliação.

Hipóteses devem ser registradas como hipóteses. Um finding só deve ser tratado como vulnerabilidade confirmada quando houver evidência compatível com a afirmação.

## Horizontes futuros

Os itens desta seção pertencem ao horizonte **Future** e mantêm estado de entrega **Planned**. Sua ordem poderá ser refinada conforme evidências de produto, risco e dependências.

### Open Finance somente leitura

Open Finance deverá começar como leitura e importação, não como meio para iniciar pagamento, transferência ou qualquer movimentação financeira. Os objetivos são:

- enriquecer a visão financeira;
- reduzir entrada manual;
- melhorar reconciliação;
- alimentar contexto autorizado.

Consentimento, revogação, proveniência e deduplicação são pré-condições conceituais.

### Personalização avançada

A direção detalhada está em [Personalização](personalization.md). Ela inclui Personal Financial Model, baseline individual, memória controlável, padrões, preferências confirmadas, “O que o J.A.R.V.I.S. sabe sobre mim”, personalidade e frequência de nudges.

O ciclo permanece:

**Observar → Inferir → Sugerir → Confirmar.**

### Proveniência, reconciliação e deduplicação

Com registro manual, WhatsApp, Open Finance, importações e integrações, um evento poderá aparecer em mais de uma origem. O produto deverá preservar:

- origem e confiança associada;
- classificação;
- reconciliação e deduplicação;
- revisão humana quando houver ambiguidade;
- prevenção de dupla contagem.

Informações ambíguas nunca devem ser fundidas ou apagadas silenciosamente.

### Áudio e fotos

Áudio e fotos poderão reduzir o atrito da entrada de dados. Extração, OCR ou interpretação visual não devem ser tratados como fontes infalíveis. Informações financeiras extraídas precisam de interpretação, revisão e confirmação apropriadas antes da persistência.

### Android

O iOS permanece como primeiro canal nativo. Um futuro Android deverá reutilizar conceitos, contratos, linguagem visual, princípios de produto e regras financeiras, respeitando as convenções da plataforma sem exigir equivalência visual exata.

### MCP

MCP poderá existir como adaptador futuro sujeito aos mesmos controles de autorização, isolamento, auditabilidade e limites da IA. Ele não deve se tornar um atalho para acesso irrestrito a dados financeiros.

### Cloud

Infraestrutura cloud deverá entrar progressivamente, conforme necessidade real. A preferência permanece pelo modular monolith, pela simplicidade operacional e por evolução baseada em evidência, em coerência com a [visão de arquitetura](../architecture/overview.md). Arquitetura distribuída prematura não é um objetivo.

## Sequência e dependências estratégicas

```text
Fundação
  ↓
Despesas
  ↓
Receitas
  ↓
Categorias, histórico e recorrências
  ↓
Cartões, parcelas e compromissos futuros
  ↓
Orçamento e Disponível Seguro
  ↓
Metas e simulações
  ↓
Identidade, autenticação e Trust Center
  ↓
WhatsApp
  ↓
Assessor com IA
  ↓
Hardening para uso real ampliado
```

Essa sequência representa a ordem estratégica predominante, não uma dependência rígida de engenharia. Descobertas podem alterar prioridades, e trabalhos transversais devem ocorrer antes sempre que o risco ou a qualidade exigirem.

## Trilhas transversais

As seguintes disciplinas evoluem continuamente e não devem aguardar um incremento específico:

- segurança e privacidade;
- QA e documentação;
- acessibilidade e performance;
- observabilidade básica;
- threat modeling;
- auditoria proporcional ao risco.

Cada incremento deve aplicar os gates pertinentes, registrar evidências e preservar as baselines especializadas.

## Gates conceituais

Os gates abaixo orientam decisões de avanço; não constituem certificação formal.

### Antes de uso financeiro pessoal mais sério

- controles mínimos de segurança compatíveis com o risco;
- uso de dados sintéticos quando apropriado;
- backup e recovery adequados ao contexto;
- testes consistentes e evidências reproduzíveis.

### Antes de multiusuário

- autenticação e autorização;
- isolamento por `user_id`;
- testes de IDOR e de fronteiras entre usuários;
- controles de privacidade.

### Antes de WhatsApp

- vínculo seguro e identidade;
- autorização;
- deduplicação;
- confirmação explícita de registros financeiros.

### Antes de IA

- ferramentas estruturadas;
- isolamento entre IA e banco de dados;
- Financial Engine confiável;
- proveniência;
- boundaries de Policy/Security definidos.

### Antes de Open Finance

- consentimento e revogação;
- proveniência;
- reconciliação e deduplicação;
- tratamento seguro de dados importados.

### Antes de beta externo

- preparação para LGPD proporcional ao tratamento pretendido;
- security hardening;
- recovery testado;
- acessibilidade verificada;
- performance validada;
- observabilidade adequada;
- revisão de segurança e pentest conforme o risco.

## Proteção de foco

> “O J.A.R.V.I.S. não precisa conhecer toda a sua vida. Ele precisa entender profundamente a parte dela que afeta suas decisões financeiras.”

O roadmap não expande o produto para tarefas, hábitos, calendário, notas ou produtividade genéricos, nem para um superapp de vida. Contexto externo só é pertinente quando melhora uma decisão financeira e respeita autorização, finalidade e minimização.

## Filtro para novas capacidades

Antes de acrescentar uma capacidade ao roadmap, convém perguntar:

1. Resolve um problema financeiro real?
2. Melhora organização, compreensão ou decisão?
3. Respeita o princípio de não movimentar dinheiro?
4. Consegue ser explicável?
5. Preserva o controle do usuário?
6. Cabe no modelo de segurança?
7. Justifica a complexidade adicionada?
8. Fortalece a diferenciação ou apenas copia o mercado?

Esse filtro orienta decisões de produto; não é um processo burocrático obrigatório. A [análise competitiva](competitive-analysis.md) ajuda a distinguir aprendizado de simples reprodução, e os [princípios de design](design-principles.md) orientam a coerência da experiência.

## Resultado estratégico

O roadmap organiza uma evolução progressiva da capacidade de registrar para a capacidade de aconselhar e personalizar. O resultado desejado é um assessor financeiro contextual, explicável e controlável, sem iniciar, autorizar ou executar transações financeiras.

O J.A.R.V.I.S. continua ajudando o usuário a compreender seu dinheiro e tomar decisões melhores. A decisão final permanece com o usuário.
