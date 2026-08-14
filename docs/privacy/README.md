# Privacidade

Estes documentos são baselines e templates de privacy by design. Eles não afirmam conformidade jurídica completa, não registram tratamentos inexistentes e não substituem revisão jurídica.

## Estado atual

É necessário distinguir três dimensões:

- **Campos modelados:** `Expense` já representa ID, UserID, descrição, valor, forma de pagamento, data/hora, timezone financeiro e origem. UserID é um identificador opaco do modelo e não é considerado intrinsecamente anônimo ou sintético.
- **Fixtures de teste:** os valores usados atualmente pelos testes automatizados são exclusivamente sintéticos.
- **Operação local/teste:** PostgreSQL agora persiste dados financeiros sintéticos em desenvolvimento e em bancos descartáveis de integração. Isso valida comportamento técnico, não representa tratamento de usuário real.
- **Tratamento de produto:** ainda não existem conta ou usuário real, armazenamento de dados financeiros reais, endpoint/canal financeiro ou integração externa. O runtime HTTP não coleta nem persiste esses campos.

Enquanto o projeto for usado exclusivamente por pessoa natural para fins particulares e não econômicos, aplica-se a exceção do art. 4º, I, da LGPD. Essa exceção deve ser reavaliada se o contexto mudar e nunca autoriza ignorar segurança ou privacidade.

Qualquer beta com terceiros exige a conclusão e aprovação formal do [LGPD Readiness Gate](privacy-by-design-checklist.md) antes de usuários externos.

## Documentos

- [Baseline LGPD](lgpd-baseline.md)
- [Inventário de dados](data-inventory.md)
- [Atividades de tratamento](processing-activities.md)
- [Bases legais](legal-bases.md)
- [Retenção e eliminação](retention-and-deletion.md)
- [Direitos dos titulares](data-subject-rights.md)
- [Operadores e transferências](processors-and-transfers.md)
- [Checklist privacy by design e readiness gate](privacy-by-design-checklist.md)
- [Template de RIPD](ripd-template.md)

Referências oficiais: [Lei nº 13.709/2018](https://www.planalto.gov.br/ccivil_03/_ato2015-2018/2018/lei/l13709.htm) e [orientações da ANPD](https://www.gov.br/anpd/pt-br).
