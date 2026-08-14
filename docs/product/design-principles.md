# Princípios de design e experiência

- Maturidade documental: **Proposed**
- Estado da capacidade: **Planned**

## Propósito e escopo

Este documento define a direção conceitual de experiência, UX e linguagem visual do J.A.R.V.I.S. Ele orienta evolução futura, mas não é especificação final de interface, design system implementado ou ordem de execução.

A UI do Incremento 1 permanece como fundação funcional. Estes princípios não exigem seu redesenho imediato nem alegam que a experiência futura descrita já esteja disponível.

O [Product Book](product-book.md) define a visão estratégica; [Princípios do assessor](advisor-principles.md) orientam comportamento e linguagem; [Personalização](personalization.md) define contexto individual futuro; e a [Análise competitiva](competitive-analysis.md) registra referências e aprendizados sem autorizar cópia.

## Direção central

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

O produto deve parecer tecnologicamente avançado sem ser barulhento, agressivo, exageradamente futurista, gamer, experimental ou difícil de compreender. Sofisticação deve vir de clareza, coerência e atenção aos detalhes, não de ornamentação.

## Uma experiência, diferentes perspectivas

> “O usuário não deve sentir que está navegando por funcionalidades. Deve sentir que está conversando com o mesmo assessor, enquanto muda a forma de olhar para sua vida financeira.”

Histórico, orçamento, metas, cartões, compromissos, Disponível Seguro, simulações e conversa não devem parecer miniapps independentes. Eles devem funcionar futuramente como perspectivas da mesma inteligência financeira.

Essa continuidade exige:

- contexto preservado quando relevante e autorizado;
- navegação coerente;
- componentes consistentes;
- transições compreensíveis;
- informação persistente quando ajuda a decisão;
- hierarquia visual comum;
- mesma personalidade em toda a experiência;
- terminologia financeira estável entre áreas e canais.

Consistência não significa tornar todas as telas iguais. Cada perspectiva pode adaptar sua representação à pergunta do usuário, desde que preserve a relação com o todo.

## Conversa como parte da interface

> “A interface pode mostrar dados. O assessor deve compreender contexto.”

O J.A.R.V.I.S. não deve parecer um dashboard com um chatbot anexado. A conversa é uma forma de acessar, compreender e aprofundar o mesmo sistema financeiro que também pode ser explorado visualmente.

A experiência futura poderá combinar:

- toque;
- texto;
- voz;
- ações rápidas;
- sugestões contextuais;
- gráficos;
- números;
- explicações;
- simulações.

O usuário não deveria precisar conhecer a estrutura de navegação para formular perguntas como:

- “Por que meu Disponível Seguro caiu?”
- “Quanto estou gastando acima do normal?”
- “Posso comprar um notebook de R$ 5.000 em 10x?”
- “Qual compromisso está pesando mais no próximo mês?”

A resposta pode começar de forma conversacional e permitir expansão progressiva para:

- composição do cálculo;
- histórico;
- gráfico;
- transações;
- compromissos;
- hipóteses;
- próximos passos.

Conversa não substitui inspeção. Visualização não substitui explicação. Ambas devem apontar para a mesma verdade financeira.

## Complexidade progressiva

Divulgação progressiva (*progressive disclosure*) é um princípio importante: mostrar primeiro o necessário para compreender e decidir, oferecendo profundidade conforme o interesse do usuário.

A Home não deve tentar apresentar toda a vida financeira simultaneamente. Sua prioridade conceitual é:

1. situação financeira atual;
2. o que merece atenção;
3. próxima decisão relevante;
4. acesso natural ao assessor.

Exemplo meramente conceitual de hierarquia, sem definir conteúdo obrigatório ou layout final:

```text
Boa noite, <usuário>.

Seu mês continua saudável.

R$ X
Disponível Seguro

Mudou R$ Y desde ontem.
[Por quê?]

O que você quer saber?

[Posso comprar algo?]
[Como estou este mês?]
[O que mudou?]
```

Informação adicional deve aparecer quando melhora a compreensão, não para demonstrar quantidade de dados disponível.

## Direção visual dark-first

Dark-first é uma direção a explorar e validar, não uma obrigação absoluta nem licença para ignorar preferências e convenções da plataforma.

Possibilidades conceituais incluem:

- fundo grafite ou preto profundo;
- superfícies discretamente elevadas;
- contraste controlado;
- azul ou ciano como possível assinatura principal;
- luminosidade sutil;
- gradientes pontuais;
- sombras discretas quando necessárias;
- espaço negativo;
- hierarquia clara.

Não existe paleta final, código hexadecimal, fonte ou tratamento visual aprovado nesta etapa. Azul e ciano são hipóteses de identidade, não requisitos imutáveis. A direção deve ser testada com acessibilidade, legibilidade, contexto de uso e configurações do sistema.

## Cor com propósito

Cor deve reforçar significado, hierarquia ou interação:

- **azul/ciano:** possível identidade, inteligência, interação ou foco;
- **verde:** estado financeiro semanticamente positivo, quando adequado;
- **vermelho:** risco, erro ou perda quando necessário;
- **amarelo/âmbar:** atenção;
- **neutros:** estrutura, hierarquia e leitura.

Nenhum significado pode depender exclusivamente de cor. Vermelho e verde não devem se tornar decoração constante, e categorias financeiras não devem produzir uma explosão cromática que prejudique leitura e consistência.

## Efeitos visuais

O J.A.R.V.I.S. pode transmitir tecnologia sem transformar a interface em espetáculo.

Evitar:

- excesso de glow;
- neon em todos os elementos;
- bordas luminosas constantes;
- partículas decorativas;
- HUD de ficção científica;
- hologramas gratuitos;
- glassmorphism excessivo;
- transparência que prejudique leitura.

Efeitos devem ser raros, intencionais e subordinados ao conteúdo.

> Sofisticação antes de espetáculo.

## Superfícies e cards

Cards podem ajudar a agrupar uma unidade de conteúdo ou ação, mas não são solução automática para toda informação.

Evitar:

- card dentro de card dentro de card;
- dezenas de caixas independentes;
- dashboard formado apenas por tiles;
- bordas fortes em todos os componentes.

Preferir agrupamento por proximidade, hierarquia, espaço, superfícies sutis e continuidade. Um card deve existir porque esclarece estrutura, não apenas porque é visualmente conveniente.

## Tipografia e números

A hierarquia tipográfica deve permitir identificar rapidamente:

- o que está acontecendo;
- quanto importa;
- o que mudou;
- o que merece ação.

Valores financeiros relevantes podem receber destaque significativo, mas nem todo número deve competir pela atenção principal.

Princípios:

- leitura rápida;
- hierarquia previsível;
- texto de apoio discreto, mas legível;
- valores e unidades compreensíveis;
- formatação monetária consistente;
- suporte integral a Dynamic Type.

Nenhuma fonte final é escolhida nesta etapa.

## Gráficos que respondem perguntas

Antes de incluir um gráfico, a pergunta deve ser:

> “Que decisão ou compreensão isso melhora?”

Princípios para visualizações financeiras:

- apresentação minimalista;
- escalas honestas;
- contexto suficiente;
- comparação relevante;
- legenda somente quando necessária;
- interação acessível;
- alternativa textual;
- resumo interpretável pelo assessor.

Em vez de apenas exibir uma linha, a experiência pode explicar:

> “Seus gastos estão 18% acima do seu padrão para esta altura do mês.”

O gráfico complementa a explicação e permite investigação. Informação material nunca pode ficar disponível exclusivamente em uma representação visual.

## Movimento com propósito

Motion deve ajudar a explicar:

- transição;
- relação;
- mudança;
- resultado;
- feedback.

Evitar animação constante, elementos pulsando sem necessidade, delays artificiais, transições longas e celebrações exageradas em contexto financeiro.

O movimento deve ser curto, funcional, previsível e interrompível quando aplicável. A experiência deve respeitar Reduce Motion integralmente.

## Estados financeiros sem julgamento

Estados visuais devem comunicar situação, consequência, importância e possibilidade de ação. Um mês acima do orçamento não deve fazer a interface parecer que o usuário “falhou”.

Evitar linguagem visual punitiva, alarmismo decorativo e celebrações que minimizem riscos. O tratamento deve ser coerente com os [Princípios do assessor](advisor-principles.md), que proíbem culpa, constrangimento e moralização da condição financeira.

## Home

A Home deve priorizar compreensão imediata, contexto, relevância, continuidade e próxima ação possível. Ela é ponto de entrada para o assessor, não um painel de controle de avião.

Evitar uma Home com:

- dezenas de indicadores;
- todos os gráficos existentes;
- todas as contas simultaneamente;
- inúmeras notificações;
- atalhos sem hierarquia.

Este documento não define seu layout final.

## Disponível Seguro

Disponível Seguro poderá futuramente receber destaque visual importante, mas não deve aparecer somente como um número gigante.

A UX deve oferecer um caminho natural:

> número → explicação → composição → detalhe

O usuário deve poder perguntar “Por quê?” e compreender compromissos, proteções, hipóteses, dados ausentes e mudanças relevantes. O conceito estratégico e suas limitações permanecem no [Product Book](product-book.md).

## Simulador “Posso comprar?”

O futuro simulador deve parecer uma conversa sobre consequências financeiras, não uma calculadora isolada.

Exemplo conceitual:

> Usuário: “Posso comprar um notebook de R$ 5.000 em 10x?”

A experiência poderá combinar:

- resposta resumida;
- impacto no Disponível Seguro;
- impacto nos próximos meses;
- metas afetadas;
- compromissos futuros;
- hipóteses;
- possibilidade de explorar alternativas.

O exemplo não define interface, fórmula ou decisão automática. O usuário continua responsável pela escolha.

## Acessibilidade como design

Acessibilidade faz parte da linguagem visual desde o início; não é etapa posterior de correção. WCAG 2.2 nível AA continua sendo a baseline formal do projeto, detalhada na [documentação de acessibilidade](../accessibility/).

Princípios:

- contraste suficiente;
- Dynamic Type;
- VoiceOver e semântica nativa;
- ordem de foco previsível;
- alvos de toque adequados;
- Reduce Motion;
- alternativa textual para gráficos;
- estados que não dependem somente de cor;
- linguagem compreensível.

Dark-first nunca pode justificar contraste ruim. Minimalismo nunca pode justificar ausência de informação necessária. Testes automatizados não substituem validação manual com tecnologias assistivas.

## Privacidade na apresentação

Informações financeiras são sensíveis. A experiência futura deve considerar:

- conteúdo de notificações;
- exposição no seletor de aplicativos;
- widgets;
- capturas de tela;
- valores visíveis em espaços públicos;
- ocultação de valores quando desejada;
- estados bloqueados;
- telas de autenticação.

Esta seção registra privacy-aware presentation como princípio, não escolhe soluções técnicas. Requisitos e decisões especializadas permanecem na [documentação de privacidade](../privacy/) e na [baseline de segurança](../security/baseline.md).

## App como interface de autoridade

A direção de canais permanece:

> WhatsApp = conveniência.
>
> App = autoridade para ações e decisões sensíveis.

O app deve oferecer futuramente maior transparência, profundidade, confirmação, controle, inspeção e gerenciamento de confiança. Isso não afirma que autenticação avançada ou WhatsApp funcional já estejam implementados.

## Estados de erro

Erros devem ser claros, acionáveis, sem culpa, sem humor inadequado e sem linguagem técnica desnecessária.

Quando possível e seguro, a experiência deve diferenciar:

- erro de entrada;
- indisponibilidade;
- ausência de dados;
- incerteza;
- permissão necessária;
- conflito;
- falha de sincronização.

Uma mensagem genérica não deve esconder orientação útil que possa ser oferecida com segurança.

## Loading e IA

O produto não deve criar artificialmente a sensação de “IA pensando”. Evitar delays falsos, digitação teatral longa e animações que simulem raciocínio inexistente.

Quando o processamento levar tempo, a interface deve comunicar o estado real, permitir continuidade quando possível e evitar bloqueio desnecessário. Honestidade temporal também sustenta confiança.

## Design system futuro

Um design system próprio é trabalho futuro **Planned**. Ele deverá eventualmente definir:

- cores semânticas;
- tipografia;
- espaçamento;
- superfícies;
- elevação;
- raios de canto;
- iconografia;
- linguagem visual financeira;
- linguagem de gráficos;
- motion;
- loading;
- estados vazios;
- erros;
- componentes conversacionais;
- estados sensíveis à privacidade;
- comportamento de acessibilidade.

Tokens semânticos devem ser preferidos a valores espalhados pela UI. Exemplos meramente conceituais, sem representar nomes finais:

```text
surface.primary
surface.elevated
text.primary
text.secondary
accent.jarvis
finance.positive
finance.negative
finance.warning
finance.protected
```

Os nomes, valores, componentes e critérios finais somente devem ser escolhidos na etapa própria de design system, acompanhados de validação visual e de acessibilidade.

## Identidade multiplataforma

A identidade deve nascer conceitualmente preparada para:

- iPhone;
- futuro Android;
- futuro web;
- widgets;
- notificações;
- possíveis superfícies futuras.

Consistência não exige reprodução pixel a pixel. Convenções nativas devem ser respeitadas quando melhorarem acessibilidade, compreensão, segurança ou familiaridade. A identidade precisa ser reconhecível sem lutar contra a plataforma.

No WhatsApp futuro, a continuidade não dependerá da mesma UI do app, mas de personalidade, linguagem, conceitos, terminologia, comportamento e consistência das explicações. O usuário deve sentir que está falando com o mesmo J.A.R.V.I.S.

## Referências e identidade própria

A [Análise competitiva](competitive-analysis.md) usa Néctar, Cleo, Copilot Money e outros produtos como referências de aprendizado. Néctar é especialmente útil para observar integração visual, módulos conectados, continuidade, dark theme, uso de cor e proximidade entre conversa e informação.

A referência é a **sensação de coesão**, não a reprodução da solução visual. Não copiar:

- layout;
- navegação específica;
- componentes;
- paleta;
- assets;
- ícones;
- ilustrações;
- identidade;
- marca;
- animações distintivas;
- textos;
- trade dress.

A identidade do J.A.R.V.I.S. deve ser própria e permanecer especializada na vida financeira do usuário.

## Anti-padrões

Evitar que o produto se torne:

- banco tradicional visualmente;
- ERP;
- dashboard corporativo;
- planilha com decoração;
- chatbot com gráficos anexados;
- interface gamer;
- HUD futurista;
- coleção de cards;
- superapp visualmente incoerente.

Também evitar:

- excesso de informação;
- excesso de notificações;
- excesso de animação;
- excesso de cor;
- excesso de indicadores;
- excesso de modais;
- microinterações decorativas sem função.

## Princípios canônicos resumidos

1. Clareza antes de espetáculo.
2. Contexto antes de quantidade.
3. Explicação antes de opacidade.
4. Continuidade antes de fragmentação.
5. Hierarquia antes de densidade.
6. Semântica antes de decoração.
7. Movimento com propósito.
8. Acessibilidade desde o início.
9. Privacidade visível.
10. O usuário continua no controle.

## Decisões ainda não tomadas

Este documento deliberadamente não define:

- paleta ou códigos de cor finais;
- fonte;
- valores de espaçamento, raio, elevação ou tamanho;
- layout da Home;
- componentes finais;
- biblioteca de gráficos;
- estilo definitivo de ícones, ilustrações ou motion;
- aparência final da conversa;
- comportamento visual detalhado por plataforma;
- design system implementado;
- cronograma de redesign.

Essas decisões exigem exploração, prototipação, testes com usuários, validação de acessibilidade e evidência própria. Até lá, este documento orienta intenção sem transformar direção em especificação final.
