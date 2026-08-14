# Retenção, anonimização e eliminação

Estado: política para dados reais **PLANEJADA**; não existem prazos de retenção aprovados. Testes de integração persistem somente fixtures sintéticas em volume descartável removido automaticamente. O volume de desenvolvimento local aceita somente dados sintéticos, é preservado por `make db-down` e pode ser eliminado explicitamente com `docker compose down --volumes`; isso é lifecycle técnico, não uma política de retenção de produto.

Cada tratamento futuro deve definir, antes da coleta:

- evento inicial e critério de retenção;
- justificativa de necessidade e obrigações aplicáveis;
- mecanismo de eliminação ou anonimização;
- tratamento de cópias, backups, logs e caches;
- responsáveis, prazo operacional e evidência do atendimento;
- exceções documentadas e revisadas.

Prazos não serão inventados sem finalidade, requisito e justificativa. Retenção indefinida por conveniência é proibida.
