# Site Oficial da Linguagem Noxy

Este é o site oficial da linguagem de programação **Noxy**, uma linguagem estaticamente tipada com VM baseada em pilha (Go).

## 🦉 Sobre o Mascote

Nossa coruja roxa é o símbolo da linguagem Noxy, representando:
- **Sabedoria**: Como as corujas, Noxy promove decisões inteligentes no desenvolvimento
- **Visão noturna**: Capacidade de "ver" bugs e problemas que outros não conseguem
- **Elegância**: Código limpo e bem estruturado
- **Silêncio**: Compilação eficiente sem barulho desnecessário

## 🚀 Tecnologias Utilizadas

- **HTML5**: Estrutura semântica moderna
- **CSS3**: Design responsivo com Grid e Flexbox
- **JavaScript ES6+**: Interatividade e animações
- **Prism.js**: Syntax highlighting para códigos
- **Font Awesome**: Ícones modernos
- **GitHub Pages**: Hospedagem gratuita

## 📱 Funcionalidades

- ✅ Design responsivo para todos os dispositivos
- ✅ Navegação suave entre seções
- ✅ Exemplos de código interativos
- ✅ Syntax highlighting para Noxy
- ✅ Animações e transições suaves
- ✅ Página 404 personalizada
- ✅ SEO otimizado
- ✅ Acessibilidade (WCAG 2.1)

## 🎨 Paleta de Cores

- **Primária**: `#6366f1` (Indigo)
- **Secundária**: `#8b5cf6` (Roxo - cor da coruja)
- **Accent**: `#06b6d4` (Cyan)
- **Texto**: `#1f2937` (Cinza escuro)

## 🏗️ Estrutura do Projeto

```
docs/
├── index.html          # Página principal
├── styles.css          # Estilos CSS
├── script.js           # JavaScript
├── 404.html           # Página de erro
├── *.md               # Documentação (spec, guias) — renderizada pelo Jekyll
└── README.md          # Este arquivo (excluído do site)
```

## 📦 Deployment

O site é publicado pelo GitHub Pages (branch `main`, pasta `/docs`) a cada push. O Jekyll do GitHub Pages renderiza também os `.md` desta pasta (plugin `jekyll-optional-front-matter`), então todo Markdown passa pelo Liquid: texto com `{{`/`{%` literal (como a seção de f-strings da spec) precisa ficar entre `<!-- {% raw %} -->` e `<!-- {% endraw %} -->`.
Acesse em: [https://noxylang.com/](https://noxylang.com/)

## 🤝 Contribuindo

Para contribuir com o site:

1. Fork o repositório
2. Crie uma branch para sua feature
3. Faça suas alterações
4. Teste localmente
5. Envie um Pull Request

## 📄 Licença

Este projeto é para fins educacionais.

---

🦉 **Desenvolvido com sabedoria pela comunidade Noxy**
