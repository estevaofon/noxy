---
name: GitHub Login
description: Instruções e comando para realizar o login no GitHub CLI (gh) localmente no container via variável de ambiente.
---

# GitHub Login

Sempre que você precisar interagir com o GitHub usando a linha de comando (`gh`) e não estiver autenticado no container local, utilize a variável de ambiente configurada:

## Passos

1. O Devcontainer está configurado para injetar automaticamente a variável `GITHUB_TOKEN` do arquivo `.env`. Para realizar o login silenciosamente, basta rodar:
   ```bash
   echo "$GITHUB_TOKEN" | gh auth login --with-token
   ```

2. (Opcional) Verifique se a autenticação foi realizada com sucesso:
   ```bash
   gh auth status
   ```

**Nota:** Você não precisa pedir ao usuário para passar nenhum token manualmente, pois ele já é mapeado do host diretamente para o container.
