# Registro de atividades de tratamento

Estado: tratamento de produto com dados reais **PLANEJADO** e inexistente. Existe somente validação técnica local/CI com fixtures sintéticas em PostgreSQL; ela não cria conta, usuário externo, coleta ou compartilhamento.

| Atividade | Dados | Ambiente | Estado | Observação |
| --- | --- | --- | --- | --- |
| Teste da persistência de Expense | Fixtures sintéticas do inventário | Máquina do desenvolvedor e runner CI descartável | IMPLEMENTADO | Banco por teste; cleanup automático de integração; sem dado pessoal real |
| Registro de Expense de usuário real | A definir | A definir | PLANEJADO | Bloqueado pelo LGPD Readiness Gate e por decisões ainda ausentes |

Cada atividade futura deve registrar:

| Campo | Conteúdo esperado |
| --- | --- |
| Identificador e estado | Nome estável; PLANEJADO, IMPLEMENTADO ou VERIFICADO |
| Papéis | Controlador, operadores e responsabilidades confirmadas |
| Titulares e dados | Referências ao inventário, sem copiar dados reais |
| Finalidade | Objetiva, específica e aprovada |
| Operações e fluxo | Coleta, uso, armazenamento, compartilhamento e eliminação |
| Base legal | Referência à análise documentada, nunca presumida |
| Retenção | Critério e justificativa aprovados |
| Controles e riscos | Salvaguardas técnicas e organizacionais |
| Evidência | Contrato, teste, revisão e aprovação aplicáveis |

Não extrapolar o teste sintético para afirmar tratamento real, base legal, fornecedor, transferência, retenção ou conformidade.
