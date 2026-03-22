---
description: Implementa uma nova funcionalidade de ponta a ponta (Branch, Código, Teste e PR)
---
Este comando é usado para adicionar novas funcionalidades ao projeto. Ele engloba desde a criação da branch, planejamento, implementação, validação, até a abertura do PR.

**Feature:** $INPUT

---

## ⚠️ Regras de Execução Obrigatórias no ARC

1. **Validação de entrada**: Se `$INPUT` estiver vazio ou não descrever claramente uma feature, pergunte ao usuário para descrever a funcionalidade desejada antes de prosseguir.
2. **Checklist Obrigatório**: Em sua PRIMEIRA resposta, gere um plano no formato esperado pelo ARC (lista com `- [ ]` e `- [x]`) contendo todas as etapas abaixo. Conforme você avança, vá marcando com `[x]`. 
3. **Nunca encerrar com pendências**: Certifique-se de executar e checar a Etapa 7 (Verificação Final) antes de encerrar.
4. **Ferramentas**: Utilize a ferramenta `bash` para comandos do git, testadores e GitHub CLI.

---

## Pré-condições

Antes de iniciar, valide usando a ferramenta `bash`:
- Estar na branch `develop` e com working tree limpa (`git status` limpo)
- Ter `gh` CLI autenticado (`gh auth status`)
- `$INPUT` não pode estar vazio

Se alguma pré-condição falhar, resolva antes de prosseguir (ex: stash de mudanças pendentes, checkout para develop).

---

## O Fluxo de Trabalho (Siga nesta ordem estrita)

**Etapa 0 — Git Identity e GitHub Login**
- Verifique se `user.name` e `user.email` estão configurados (`git config user.name`). 
- Verifique a autenticação do CLI do GitHub (`gh auth status`). Realize login se necessário.

**Etapa 1 — Criar branch e tasks**
- Crie uma branch a partir da `develop` no padrão: `feat/<topic>` (kebab-case, descritivo). Use a ferramenta `bash`.

**Etapa 2 — Planejar**
- Analise o fluxo existente (módulos relacionados) pesquisando o codebase (`code_structure`, `glob_search`, `read_file`).
- Desenhe a estratégia de implementação.

**Etapa 3 — Implementar**
- Desenvolva a feature na branch criada (`edit_file`, `write_file`).
- Siga os padrões de código do projeto.
- Crie ou atualize testes unitários.

**Etapa 4 — Validar**
- Rode os testes, build, lint ou validação aplicável (ex: `bash("python -m pytest")` ou `bash("npm run build")`).
- Corrija problemas se houver.

**Etapa 5 — Commit**
- Faça o commit seguindo *Conventional Commits*: `feat(<scope>): descrição curta`. Use `bash`.

**Etapa 6 — Abrir PR**
- Faça push da branch (`git push -u origin HEAD`).
- Abra o PR usando `bash("gh pr create --title '...' --body '...'")`.
- Retorne o link do PR no chat.

**Etapa 7 — Verificação Final**
- Revise seu próprio checklist de planejamento. Todas as etapas acima foram feitas? Se sim, mostre um pequeno resumo (walkthrough) e seja polido na conclusão.
