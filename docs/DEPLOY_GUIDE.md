# 🚀 Guia Completo de Deploy - Site Noxy

## 📋 Pré-requisitos

- ✅ Conta no GitHub
- ✅ Git configurado localmente
- ✅ Repositório criado no GitHub

## 🔄 Passo a Passo para Deploy

### 1. **Verificar Status Atual**
```bash
git status
git branch
```

### 2. **Commit das Alterações (se necessário)**
```bash
# Adicionar todos os arquivos
git add .

# Fazer commit
git commit -m "feat: Site completo da linguagem Noxy

- Site moderno com tema escuro e fonte Inter
- Mascote coruja roxa em destaque  
- Animação interativa da coruja voando
- Seções: recursos, sintaxe, exemplos, instalação
- PWA support e SEO otimizado
- GitHub Actions para deploy automático"
```

### 3. **Push para o GitHub**
```bash
# Se for primeira vez (substitua pelo seu repositório)
git remote add origin https://github.com/SEU-USUARIO/noxy.git

# Push da branch atual
git push origin feat/github-pages

# OU se quiser usar main/master
git checkout main
git merge feat/github-pages
git push origin main
```

### 4. **Configurar GitHub Pages**

#### No GitHub.com:
1. **Acesse seu repositório** no GitHub
2. **Clique em "Settings"** (Configurações)
3. **Role até "Pages"** no menu lateral
4. **Em "Source"** selecione **"GitHub Actions"**
5. **Salve as configurações**

### 5. **Deploy Automático**

O GitHub Actions vai executar automaticamente:
- ✅ **Trigger**: Push na branch main/master
- ✅ **Arquivo**: `.github/workflows/deploy.yml`
- ✅ **Processo**: Upload da pasta `docs/` para GitHub Pages
- ✅ **URL**: `https://SEU-USUARIO.github.io/noxy/`

## 📁 Estrutura do Projeto

```
noxy/
├── docs/                    # Site (será deployado)
│   ├── index.html
│   ├── styles.css
│   ├── script.js
│   ├── 404.html
│   ├── manifest.json
│   ├── robots.txt
│   ├── sitemap.xml
│   └── _config.yml
├── .github/workflows/       # GitHub Actions
│   └── deploy.yml
├── noxy_examples/          # Exemplos da linguagem
├── compiler.py             # Compilador Noxy
└── README.md
```

## 🔧 Configurações Necessárias

### Atualizar URLs nos Arquivos

#### 1. `docs/sitemap.xml`
```xml
<!-- Trocar "seu-usuario" pelo seu username -->
<loc>https://SEU-USUARIO.github.io/noxy/</loc>
```

#### 2. `docs/robots.txt`
```
Sitemap: https://SEU-USUARIO.github.io/noxy/sitemap.xml
```

#### 3. `docs/_config.yml`
```yaml
url: "https://SEU-USUARIO.github.io"
baseurl: "/noxy"
```

## 🚀 Comandos Completos

### Se for primeiro deploy:
```bash
# 1. Verificar repositório remoto
git remote -v

# 2. Se não houver, adicionar
git remote add origin https://github.com/SEU-USUARIO/noxy.git

# 3. Push inicial
git push -u origin feat/github-pages

# 4. Opcional: mesclar com main
git checkout main
git merge feat/github-pages
git push origin main
```

### Se já existe repositório:
```bash
# 1. Commit das mudanças
git add .
git commit -m "update: Site da linguagem Noxy completo"

# 2. Push
git push origin feat/github-pages
```

## 🌐 Verificar Deploy

### 1. **GitHub Actions**
- Vá em **Actions** no seu repositório
- Veja o workflow "Deploy to GitHub Pages"
- Status: ✅ Verde = sucesso

### 2. **Acessar Site**
- URL: `https://SEU-USUARIO.github.io/noxy/`
- Demora ~2-5 minutos para ficar ativo

### 3. **Testar Funcionalidades**
- ✅ Site carrega corretamente
- ✅ Tema escuro funcionando
- ✅ Animação da coruja (clique nela!)
- ✅ Menu mobile responsivo
- ✅ Todas as seções navegáveis

## 🔄 Updates Futuros

Para atualizar o site:
```bash
# 1. Fazer mudanças nos arquivos docs/
# 2. Commit
git add .
git commit -m "update: melhorias no site"

# 3. Push (deploy automático)
git push origin main
```

## 🛠️ Troubleshooting

### ❌ Deploy falhou?
1. Verifique **Actions** no GitHub
2. Veja logs de erro
3. Confirme que pasta `docs/` existe
4. Verifique permissões do repositório

### ❌ Site não carrega?
1. Aguarde 5-10 minutos
2. Verifique configuração em **Settings > Pages**
3. Teste em aba anônima (cache)
4. Verifique console do navegador

### ❌ URLs quebradas?
1. Atualize `sitemap.xml` com seu username
2. Verifique `_config.yml`
3. URLs devem ser relativas no HTML

## 📊 Monitoramento

### Analytics (opcional)
Para adicionar Google Analytics, inclua no `<head>`:
```html
<!-- Google Analytics -->
<script async src="https://www.googletagmanager.com/gtag/js?id=GA_MEASUREMENT_ID"></script>
```

### SEO
- ✅ Sitemap.xml configurado
- ✅ Robots.txt otimizado  
- ✅ Meta tags completas
- ✅ PWA manifest

---

🦉 **Seu site da linguagem Noxy estará voando alto no GitHub Pages!**

### 🎯 Resultado Final:
- **URL pública**: `https://SEU-USUARIO.github.io/noxy/`
- **Deploy automático**: A cada push
- **Performance**: CDN global do GitHub
- **HTTPS**: Certificado automático
- **Custom domain**: Possível configurar depois
