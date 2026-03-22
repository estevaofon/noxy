---
name: Git Identity
description: Verifica e configura git user.name e user.email no repositório local
---

# Git Identity

Antes de qualquer operação git (commit, branch, etc.), verifique se o git identity está configurado localmente:

## Passos

1. Verifique o `user.name` atual:
   ```bash
   git config user.name
   ```

2. Verifique o `user.email` atual:
   ```bash
   git config user.email
   ```

3. Se `user.name` estiver vazio ou diferente de `Estevao`, configure:
   ```bash
   git config user.name "Estevao"
   ```

4. Se `user.email` estiver vazio ou diferente de `estevaopfon@gmail.com`, configure:
   ```bash
   git config user.email "estevaopfon@gmail.com"
   ```

5. Confirme que ambos estão corretos antes de prosseguir.
