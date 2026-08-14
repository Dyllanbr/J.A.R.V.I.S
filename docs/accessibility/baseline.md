# Baseline de acessibilidade

## Estado

WCAG 2.2 nível AA é a baseline mínima do J.A.R.V.I.S. A Etapa 3 possui a primeira UI SwiftUI **IMPLEMENTADA**, mas não existe alegação de conformidade: revisão independente e validação manual com tecnologias assistivas ainda não foram concluídas. Os critérios aplicáveis são mapeados ao contexto iOS/SwiftUI na matriz de rastreabilidade.

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

A primeira jornada possui regressão automatizada com identifiers semânticos independentes da copy e em `UIContentSizeCategoryAccessibilityExtraExtraExtraLarge`. Essa evidência garante alcançabilidade funcional básica com scroll; não é inspeção visual pixel a pixel nem substitui o teste manual.

## Rastreabilidade

A matriz [WCAG → requisito → componente → caso de teste → evidência](wcag-traceability-matrix.md) é o registro obrigatório. A primeira jornada possui linhas **IMPLEMENTADAS**; nenhuma pode ser classificada como VERIFICADA antes das evidências automatizadas e manuais aplicáveis.
