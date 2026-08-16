# Baseline de acessibilidade

## Estado

WCAG 2.2 nível AA é a baseline mínima do J.A.R.V.I.S. A jornada iOS de Expense do Incremento 1 está verificada; a extensão para Income e histórico misto está **IMPLEMENTADA** e aguarda auditoria global do Incremento 2. O picker de Category e os filtros locais do histórico estão **IMPLEMENTADOS** pelo Incremento 3A e aguardam auditoria final independente. Não existe alegação de conformidade: validação manual com tecnologias assistivas continua pendente. Os critérios aplicáveis são mapeados ao contexto iOS/SwiftUI na matriz de rastreabilidade.

## Requisitos para interfaces

Os critérios de aceite deverão contemplar, conforme aplicável:

- VoiceOver e semântica nativa;
- labels, values, hints e traits precisos;
- agrupamento acessível e ordem de foco previsível;
- foco visível e não obscurecido;
- Dynamic Type sem perda de conteúdo ou operação;
- Reduce Motion e alternativas a movimento não essencial;
- contraste e uso de cor que não seja a única forma de comunicar estado;
- alternativas textuais para conteúdo não textual;
- alvos de toque mensuráveis e adequados aos critérios aplicáveis e à plataforma;
- autenticação acessível quando autenticação vier a existir.

Testes automatizados apoiam regressão, mas não substituem validação manual com VoiceOver, tamanhos extremos de texto, preferências de movimento e demais tecnologias assistivas. Regressões críticas de acessibilidade bloqueiam release.

A jornada atual possui regressão automatizada com identifiers semânticos independentes da copy, distinção textual entre Entrada/Saída, Category anunciada por label, filtros identificáveis e execução em `UIContentSizeCategoryAccessibilityExtraExtraExtraLarge`. “Sem categoria” e o fallback “Categoria indisponível” são textuais, não dependem de cor ou ícone. Essa evidência garante alcançabilidade funcional básica com scroll; não é inspeção visual pixel a pixel nem substitui o teste manual.

## Rastreabilidade

A matriz [WCAG → requisito → componente → caso de teste → evidência](wcag-traceability-matrix.md) é o registro obrigatório. As extensões dos Incrementos 2 e 3A permanecem **IMPLEMENTADAS**; nenhuma pode ser classificada como VERIFICADA antes das evidências automatizadas, manuais e auditorias independentes aplicáveis.
