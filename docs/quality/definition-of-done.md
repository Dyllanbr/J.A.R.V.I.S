# Definition of Done

## Estados obrigatórios

- **PLANEJADO:** decisão ou item documentado, sem implementação.
- **IMPLEMENTADO:** código ou documentação existe, mas isso não prova correção ou conformidade.
- **VERIFICADO:** a implementação passou pelos critérios aplicáveis e cada afirmação possui referência à evidência correspondente.

## Critério para VERIFICADO

Conforme o tipo e o risco da mudança, a evidência deve cobrir build, testes unitários, integração, contrato, segurança, privacidade, acessibilidade, performance, documentação e resultados operacionais. Critérios não aplicáveis devem ser marcados como tal com justificativa; critérios futuros permanecem PLANEJADOS.

Uma entrega somente pode ser chamada de VERIFICADA quando:

1. `make verify` passa em instalação limpa e, para mudanças iOS, `make verify-ios` também passa em macOS/Simulator;
2. documentação, contrato e testes refletem o comportamento existente;
3. evidências executáveis ou relatório independente estão referenciados;
4. não existem findings bloqueadores abertos;
5. o escopo solicitado e as proibições do incremento foram respeitados.

O CI é evidência técnica reproduzível, mas não substitui revisão independente nem validações manuais exigidas por acessibilidade, privacidade ou segurança.

Para uma jornada iOS integrada, `make test-ios-integration` deve comprovar o cliente real contra API/PostgreSQL descartáveis quando aplicável. Esse resultado técnico não torna a jornada VERIFICADA antes da auditoria independente nem substitui VoiceOver/Dynamic Type/contraste manuais.
