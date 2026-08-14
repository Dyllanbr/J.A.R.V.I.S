# Product Book do J.A.R.V.I.S.

- Maturidade documental: **Proposed**
- Estado das capacidades: **misto** — Incremento 1 **Verified**; visão futura **Planned**

## Propósito do documento

Este é o documento estratégico central do J.A.R.V.I.S. Ele organiza visão, posicionamento, promessa, limites e direção futura sem substituir arquitetura, ADRs, requisitos especializados, roadmap ou backlog.

O Product Book é uma proposta documental em revisão. Seu status **Proposed** não altera o estado de entrega de nenhuma capacidade. Fatos atuais e visão futura são separados explicitamente ao longo do documento.

## Definição canônica

> “O J.A.R.V.I.S. é uma plataforma de organização, acompanhamento, análise e aconselhamento financeiro pessoal. Ele não inicia, autoriza ou executa transações financeiras ou pagamentos.”

O J.A.R.V.I.S. é construído para ajudar uma pessoa a compreender sua vida financeira, registrar o que aconteceu, acompanhar compromissos, analisar consequências e tomar decisões mais conscientes. Sua ambição não é substituir a pessoa nem a instituição financeira, mas oferecer contexto, cálculo confiável e aconselhamento explicável.

Ele não é:

- banco;
- carteira digital;
- meio de pagamento;
- iniciador de Pix;
- iniciador de transferência;
- executor de boletos;
- executor de compras;
- custodiante de dinheiro.

Princípios de confiança:

> “O J.A.R.V.I.S. entende seu dinheiro. Não toca nele.”

> “Nós aconselhamos. Você decide. Seu banco movimenta.”

> “IA para conversar. Regras para calcular. Você para decidir.”

## Problema e público

O produto é destinado a pessoas que desejam compreender e organizar a própria vida financeira sem depender de planilhas complexas, dashboards corporativos ou respostas genéricas. O problema central não é somente armazenar lançamentos: é transformar dados financeiros pessoais em contexto compreensível para decisões cotidianas.

Não existem ainda personas validadas, segmentação comercial ou pesquisa de mercado versionada. Essas definições exigirão evidência própria e não são inventadas neste documento.

## Promessa e relação com o usuário

O J.A.R.V.I.S. deve se comportar como um assessor financeiro pessoal que:

- apresenta fatos sem os alterar silenciosamente;
- distingue cálculo, inferência e sugestão;
- explica como chegou a uma orientação;
- pede confirmação antes de registrar uma movimentação;
- respeita limites de segurança e privacidade;
- mantém o usuário como responsável pela decisão final.

A relação pretendida é contínua e baseada em confiança, não em dependência. O produto deve ajudar o usuário a enxergar consequências e alternativas sem afirmar certeza quando os dados forem incompletos.

## Modelo conceitual de responsabilidades

As responsabilidades abaixo representam uma **direção planejada de produto e arquitetura**. Elas não afirmam a existência de componentes físicos separados, microserviços ou toda a capacidade descrita.

### IA / Conversation Layer

Na visão futura, a camada de conversação:

- interpreta intenção;
- conversa;
- contextualiza;
- personaliza;
- explica resultados.

A IA não é a fonte de verdade dos cálculos financeiros. Ela não deve possuir acesso irrestrito ao banco de dados nem gerar SQL para operar diretamente dados financeiros.

### Financial Engine

Na visão futura, o Financial Engine realiza cálculos financeiros determinísticos. Verdades financeiras, limites, projeções e composições não podem depender da criatividade ou da probabilidade de uma resposta de IA.

O núcleo determinístico de despesas do Incremento 1 é uma capacidade atual verificada; isso não significa que todo o Financial Engine futuro já exista.

### Policy / Security Engine

Na visão futura, o Policy / Security Engine:

- aplica políticas;
- define permissões;
- protege operações sensíveis;
- permanece soberano sobre a IA.

Essa é uma separação conceitual de responsabilidade. Nenhuma arquitetura física adicional é definida aqui.

### Usuário

O usuário mantém a decisão final. A IA pode interpretar e explicar; regras podem calcular e proteger; nenhuma delas substitui a confirmação e a escolha da pessoa.

## Estado atual — Incremento 1

O Incremento 1 está **implementado, auditado, aprovado e mergeado**. As capacidades abaixo estão **Verified** dentro do escopo e das evidências atuais:

- núcleo de domínio de despesas, incluindo Money, Expense e casos de uso;
- persistência PostgreSQL e migrations;
- audit event atômico associado ao registro da despesa;
- API financeira opt-in;
- preview sem persistência;
- confirmação explícita pelo canal antes do registro;
- idempotência persistida;
- histórico mensal owner-scoped;
- primeiro fluxo nativo iOS para registrar e consultar despesas;
- integração real iOS → backend → PostgreSQL;
- testes e quality gates correspondentes.

O escopo atual registra uma despesa já ocorrida. Ele não movimenta fundos. Não há autenticação de produto, usuário externo aprovado, banco de produção, WhatsApp funcional, IA consultiva ou Open Finance.

Detalhes e evidências permanecem nas fontes especializadas de [arquitetura](../architecture/overview.md), [ADRs](../adr/README.md), [segurança](../security/baseline.md), [privacidade](../privacy/README.md), [acessibilidade](../accessibility/baseline.md), [QA](../qa/testing-strategy.md) e [performance](../performance/baseline.md).

## Visão futura

As capacidades a seguir estão **Planned**. A presença nesta visão não define ordem, prazo, escopo técnico nem compromisso de entrega:

- receitas;
- categorias;
- recorrências e assinaturas;
- cartões e parcelas;
- compromissos futuros;
- orçamento;
- Disponível Seguro;
- metas;
- simulador “Posso comprar?”;
- autenticação;
- passkeys;
- Face ID;
- PIN J.A.R.V.I.S.;
- recovery;
- Trust Center;
- WhatsApp;
- IA consultiva;
- personalização;
- Open Finance somente leitura;
- áudio e fotos;
- Android;
- MCP seguro;
- cloud e hardening para uso real.

O [Roadmap estratégico](roadmap.md) organiza horizontes, dependências e gates. O GitHub Project continua sendo a fonte operacional do backlog.

## Canais

A visão de canais adota responsabilidades diferentes:

- **iPhone nativo:** interface de autoridade para ações, revisão e decisões sensíveis;
- **WhatsApp:** canal futuro de conveniência e entrada rápida;
- **Android:** canal futuro;
- **Open Finance:** inicialmente somente leitura e importação;
- **áudio e fotos:** entradas futuras sujeitas a validação, privacidade e confirmação;
- **MCP:** adaptador futuro submetido aos mesmos controles de segurança, política e autorização.

WhatsApp representa conveniência; o app representa autoridade. Essa divisão é direção futura e não implica que autenticação ou os canais planejados já existam.

Todo registro de movimentação financeira deve exigir confirmação explícita antes da persistência, independentemente do canal. Os métodos inicialmente reconhecidos para uma despesa são Pix, débito, crédito e dinheiro. Informar o método usado não significa que o J.A.R.V.I.S. possa executar o pagamento.

## Personalização — visão de alto nível

> “Cada usuário tem seu próprio J.A.R.V.I.S.”

> “O J.A.R.V.I.S. não conhece apenas suas contas. Ele aprende como você vive sua vida financeira.”

A visão futura inclui um modelo financeiro individual formado, de maneira controlável, por:

- histórico do próprio usuário;
- objetivos;
- preferências confirmadas;
- padrões;
- sazonalidade;
- contexto autorizado.

O guardrail conceitual é:

> Observar → Inferir → Sugerir → Confirmar

Personalização não pode alterar silenciosamente fatos financeiros, modificar cálculos determinísticos, substituir regras de segurança ou transformar inferência em fato. Ela deve ser compreensível, corrigível, revogável e controlável pelo usuário.

Os detalhes de memória, proveniência, controles e modelo financeiro individual estão em [Personalização](personalization.md). A capacidade descrita permanece **Planned**.

## De organizador a assessor financeiro

A ambição do produto vai além de responder:

> “O que aconteceu com meu dinheiro?”

Ele deve evoluir para ajudar também com:

> “O que eu deveria fazer?”

Perguntas conceituais futuras incluem:

- Posso comprar?
- Quanto posso gastar sem comprometer meus objetivos?
- Por que meu Disponível Seguro caiu?
- Qual compromisso está pesando mais?
- Se eu parcelar isso, como ficam os próximos meses?
- Estou fora do meu padrão?
- Quanto preciso ajustar para voltar à meta?

A divisão pretendida é:

> Financial Engine calcula.
>
> Personal Financial Model contextualiza.
>
> IA explica.
>
> Usuário decide.

O Personal Financial Model é visão futura e não uma implementação atual.

## Disponível Seguro — conceito estratégico

Disponível Seguro é uma capacidade futura **Planned**. Ele não representa apenas o saldo atual: considera compromissos conhecidos e proteções financeiras antes de apresentar uma referência para decisão.

O conceito deve:

- ser explicável;
- mostrar sua composição;
- declarar hipóteses e limitações;
- depender da qualidade e completude dos dados conhecidos;
- evitar aparência de garantia absoluta;
- manter a decisão sob responsabilidade do usuário.

Exemplo meramente ilustrativo, sem representar cálculo implementado ou recomendação:

```text
Saldo: R$ 3.420,00
- contas previstas: R$ 1.180,00
- fatura: R$ 720,00
- meta protegida: R$ 500,00
- margem de segurança: R$ 177,70
= Disponível Seguro: R$ 842,30
```

Fórmula, tratamento de incerteza, dados necessários e critérios serão definidos em documentação e roadmap próprios.

## Experiência integrada

> “O usuário não deve sentir que está navegando por funcionalidades. Deve sentir que está conversando com o mesmo assessor, enquanto muda a forma de olhar para sua vida financeira.”

> “A interface pode mostrar dados. O assessor deve compreender contexto.”

Histórico, orçamento, metas, Disponível Seguro, simulações e conversa devem futuramente funcionar como perspectivas do mesmo sistema financeiro pessoal, não como miniapps independentes. Cada canal pode adaptar a interação, mas deve preservar contexto autorizado, linguagem coerente, controles e continuidade.

## Foco financeiro

O J.A.R.V.I.S. não pretende se tornar um organizador genérico da vida. Ele permanece especializado na dimensão financeira.

> “O J.A.R.V.I.S. não precisa conhecer toda a sua vida. Ele precisa entender profundamente a parte dela que afeta suas decisões financeiras.”

Contextos externos somente devem ser considerados futuramente quando forem relevantes para uma decisão financeira, houver autorização adequada, os dados forem minimizados e a autorização puder ser revogada.

## Tom e personalidade

> “Personalização também significa saber quando falar — e quando ficar em silêncio.”

> “O J.A.R.V.I.S. pode brincar com a situação, mas nunca com a pessoa.”

Tom, humor, proatividade, nudges e silêncio exigem regras próprias, documentadas em [Princípios do assessor](advisor-principles.md). A capacidade permanece **Planned**. O produto não deve usar culpa, constrangimento, pressão indevida ou humor às custas do usuário.

## Direção visual

> “Tecnologia sofisticada, silenciosa e pessoal.”

A experiência futura deve transmitir:

- inteligência;
- confiança;
- calma;
- precisão;
- profundidade;
- personalização;
- continuidade;
- controle do usuário.

Ela não deve parecer:

- banco tradicional;
- ERP financeiro;
- dashboard corporativo;
- planilha sofisticada;
- chatbot com gráficos anexados;
- interface gamer ou futurista exagerada.

Dark-first, grafite/preto, azul/ciano, cards, gráficos, motion e design tokens são direções **Planned** detalhadas em [Princípios de design](design-principles.md). Nenhuma escolha visual futura substitui a baseline WCAG 2.2 AA ou as preferências de acessibilidade da plataforma.

## Concorrência e referências

Existem produtos concorrentes e adjacentes, mas “IA + finanças + WhatsApp + Open Finance” não deve ser tratado, isoladamente, como diferenciação suficiente.

A [Análise competitiva](competitive-analysis.md) registra data de revisão, fontes e distinção entre fato e hipótese. Referências de mercado servem para aprendizado, nunca para cópia de identidade, fluxos, linguagem ou interface.

## Governança e fontes especializadas

O Product Book define direção estratégica; ele não duplica as fontes especializadas:

- [arquitetura](../architecture/overview.md) e [ADRs](../adr/README.md) registram decisões técnicas;
- [segurança](../security/baseline.md) registra controles e requisitos de proteção;
- [privacidade](../privacy/README.md) registra privacy by design, LGPD e inventário;
- [acessibilidade](../accessibility/baseline.md) mantém WCAG 2.2 AA e evidências;
- [QA](../qa/testing-strategy.md) registra estratégia de testes;
- [performance](../performance/baseline.md) distingue metas de resultados medidos;
- [Definition of Done](../quality/definition-of-done.md) define os requisitos de verificação.

O GitHub Project é a fonte de verdade do backlog operacional. Este documento não deve ser convertido em cópia das Issues nem em roadmap detalhado.
