# Personalização

- Maturidade documental: **Proposed**
- Estado da capacidade: **Planned**

## Propósito e escopo

Este documento define a visão conceitual da futura personalização do J.A.R.V.I.S. Não existe hoje Personal Financial Model, memória de produto, Trust Center ou integração de contexto externo implementada. Os exemplos são sintéticos e não representam coleta, inferência ou tratamento operacional atual.

> “Cada usuário tem seu próprio J.A.R.V.I.S.”

> “O J.A.R.V.I.S. não conhece apenas suas contas. Ele aprende como você vive sua vida financeira.”

> “O J.A.R.V.I.S. não precisa conhecer toda a sua vida. Ele precisa entender profundamente a parte dela que afeta suas decisões financeiras.”

A personalização deve aprofundar o contexto financeiro necessário sem transformar o produto em organizador genérico da vida. O [Product Book](product-book.md) define a visão central, e os [Princípios do assessor](advisor-principles.md) definem como esse contexto pode ser comunicado.

## Separação de responsabilidades

> “IA para conversar. Regras para calcular. Você para decidir.”

A direção conceitual é:

> Financial Engine calcula.
>
> Personal Financial Model contextualiza.
>
> IA explica.
>
> Usuário decide.

- o **Financial Engine** mantém cálculos financeiros determinísticos;
- o **Personal Financial Model** representa contexto individual futuro;
- a **IA** interpreta intenção, conversa, contextualiza e explica;
- o **Policy / Security Engine** permanece soberano sobre permissões e políticas;
- o **usuário** confirma preferências relevantes e mantém a decisão final.

Essas são responsabilidades planejadas, não microserviços ou componentes físicos já implementados.

## Personal Financial Model

O Personal Financial Model poderá futuramente representar contexto como:

- histórico financeiro;
- comportamento recorrente;
- sazonalidade;
- objetivos;
- preferências confirmadas;
- compromissos;
- padrões;
- margem de segurança;
- tolerância a alertas;
- estilo de explicação.

Ele não substitui o Financial Engine, não altera fatos financeiros, não modifica cálculos silenciosamente e não pode sobrescrever políticas de segurança. Seu papel é oferecer contexto para explicações e sugestões proporcionais aos dados disponíveis.

## Ciclo de aprendizado

> Observar → Inferir → Sugerir → Confirmar

### Observar

O sistema identifica fatos ou padrões possíveis a partir de dados legítimos. Uma observação estatística pode existir sem transformar automaticamente uma preferência em regra permanente.

### Inferir

Uma hipótese é gerada a partir de observações, mas continua sendo hipótese. Confiança, limitações e evidência disponível precisam ser consideradas.

### Sugerir

O J.A.R.V.I.S. apresenta uma interpretação ou possibilidade ao usuário sem ocultar que se trata de sugestão ou inferência.

### Confirmar

Preferências relevantes tornam-se permanentes somente após confirmação quando apropriado. Nem todo padrão trivial precisa de confirmação explícita para existir como observação estatística, mas observação não pode ser promovida silenciosamente a preferência confirmada.

O modelo deve distinguir:

| Tipo | Significado conceitual |
| --- | --- |
| Fato financeiro | Registro ou estado sustentado por uma origem identificável |
| Observação | Resultado descritivo obtido dos dados disponíveis |
| Inferência | Hipótese que ainda pode estar errada |
| Preferência confirmada | Escolha que o usuário reconheceu ou configurou |
| Configuração explícita | Regra operacional escolhida diretamente pelo usuário |

## “O que o J.A.R.V.I.S. sabe sobre mim”

“O que o J.A.R.V.I.S. sabe sobre mim” é uma experiência futura de transparência e controle. Ela deverá permitir compreender quais contextos são utilizados e por quê, sem definir uma UI específica nesta etapa.

Exemplos conceituais e sintéticos:

### Preferências

- manter margem de segurança de R$ 1.000;
- priorizar viagem;
- evitar parcelamento acima de 6x.

### Padrões

- salário costuma entrar entre os dias 4 e 6;
- média histórica de supermercado;
- maior gasto com lazer aos sábados.

### Preferências do assessor

- alertas somente importantes;
- não alertar pequenas variações;
- explicações detalhadas para compras grandes.

O usuário deverá futuramente poder:

- visualizar;
- corrigir;
- editar;
- apagar informações relevantes.

Esses exemplos não afirmam que os dados, a memória ou os controles já existem.

## Baseline financeiro individual

O J.A.R.V.I.S. deve comparar o usuário principalmente com:

- o próprio histórico;
- a própria sazonalidade;
- os próprios objetivos;
- o próprio comportamento saudável conhecido.

Comparação com outras pessoas não deve ser o padrão central.

Exemplos conceituais futuros:

> “Você está 18% acima do seu padrão para esta altura do mês.”

> “Apesar de agosto normalmente ser um dos seus meses mais caros, você está 12% abaixo do seu padrão histórico…”

Essas afirmações somente são apropriadas quando houver quantidade, qualidade e período de dados suficientes. Sazonalidade importa; uma anomalia não é automaticamente um problema; ausência de padrão não pode ser mascarada como certeza. Um histórico prejudicial ao usuário também não deve ser tratado automaticamente como referência saudável.

## Memória controlável

A personalização deve ser transparente e controlável. Uma futura camada de memória precisa distinguir:

1. fatos financeiros;
2. padrões detectados;
3. inferências;
4. preferências confirmadas;
5. configurações explícitas.

Cada informação relevante deverá possuir, conforme aplicável:

- finalidade clara;
- proveniência;
- possibilidade de correção;
- possibilidade de exclusão;
- minimização;
- explicabilidade;
- controles apropriados do usuário.

Este documento define a experiência de produto e não replica inventário, base legal, retenção ou direitos dos titulares. Essas responsabilidades permanecem na [documentação de privacidade](../privacy/README.md).

## Proveniência

Dados financeiros e contextos relevantes devem futuramente preservar sua origem. Possíveis origens conceituais incluem:

- registro manual no iPhone;
- WhatsApp;
- Open Finance;
- importação;
- integração autorizada;
- classificação automática;
- inferência.

O sistema deve futuramente conseguir responder:

> “De onde veio esse dado?”

Proveniência deve distinguir:

- dado de origem;
- classificação;
- inferência;
- preferência confirmada.

A lista não afirma que WhatsApp, Open Finance, importações, integrações ou inferências estejam implementados.

## Contexto externo opcional

Contextos externos futuros podem incluir, como exemplos conceituais:

- calendário;
- HealthKit;
- localização.

Eles somente poderão ser considerados quando forem relevantes para uma decisão financeira, houver consentimento ou autorização adequados, os dados forem minimizados, a finalidade for explícita e a autorização puder ser revogada.

Esses exemplos não constituem compromisso de roadmap nem autorização para implementar ou coletar tais dados.

## Trust Center

O Trust Center é uma experiência conceitual futura de transparência e controle. Ele poderá concentrar:

- conexões autorizadas;
- consentimentos;
- privacidade;
- limites da IA;
- limites financeiros do J.A.R.V.I.S.;
- controles de memória;
- controles de personalização;
- controles de notificações;
- acesso a “O que o J.A.R.V.I.S. sabe sobre mim”.

O Trust Center não substitui controles técnicos, documentação de segurança, obrigações jurídicas ou mecanismos efetivos de direitos dos titulares. Esses requisitos permanecem nas fontes de [segurança](../security/baseline.md) e [privacidade](../privacy/README.md).

O J.A.R.V.I.S. não inicia, autoriza ou executa pagamentos.

## Preferências de notificações e alertas

Personalização poderá representar preferências individuais como intensidade, frequência, temas relevantes e situações que devem permanecer silenciosas. Os critérios comportamentais, o tom e os limites gerais de proatividade pertencem aos [Princípios do assessor](advisor-principles.md#proatividade-e-silêncio).

Preferências de comunicação não podem ocultar informação material obrigatória nem autorizar comportamento proibido. Nenhuma configuração de notificações está implementada nesta etapa.

## Guardrails de segurança

Personalização nunca pode:

- alterar saldo;
- alterar transação;
- alterar origem de dado;
- alterar cálculo determinístico silenciosamente;
- transformar hipótese em fato;
- ignorar política de segurança;
- autorizar ação proibida;
- esconder informação material do usuário.

A personalização fornece contexto para aconselhamento. Ela não redefine a verdade financeira.

A IA não deve possuir acesso irrestrito ao banco de dados nem gerar SQL para operar diretamente dados financeiros. Qualquer capacidade futura continua sujeita à [arquitetura](../architecture/overview.md), à [baseline de segurança](../security/baseline.md) e à [documentação de privacidade](../privacy/README.md).
