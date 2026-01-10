# Guia do Agente para o Projeto Peekaping

Este documento fornece uma visão geral do projeto Peekaping para ajudar no desenvolvimento e na manutenção contínua.

## 1. Visão Geral do Projeto

Peekaping é uma solução de monitoramento de uptime auto-hospedada, projetada como uma alternativa moderna ao Uptime Kuma. Sua arquitetura "API first" garante flexibilidade e extensibilidade.

- **Backend:** Escrito em Go, proporcionando alta performance e baixo consumo de recursos.
- **Frontend:** Desenvolvido com React e TypeScript, utilizando Vite para uma experiência de desenvolvimento rápida e moderna.
- **Banco de Dados:** Suporte flexível para SQLite, PostgreSQL e MongoDB.
- **Containerização:** Totalmente containerizado com Docker para fácil implantação e escalabilidade.

## 2. Arquitetura

O projeto é estruturado como um monorepo, gerenciado com `pnpm` e `Turborepo` para otimizar os fluxos de trabalho de desenvolvimento em múltiplas aplicações.

- `apps/`: Contém as aplicações individuais:
    - `server/`: A aplicação backend em Go.
    - `web/`: A aplicação frontend em React.
    - `docs/`: A documentação do projeto.
    - `landing/`: A landing page.
- `e2e/`: Testes end-to-end utilizando Playwright.
- `charts/`: Helm charts para implantação em Kubernetes.
- `docker-compose.*.yml`: Arquivos de configuração do Docker Compose para diferentes ambientes e bancos de dados.

A comunicação entre o frontend e o backend é baseada em uma especificação OpenAPI (`swagger.json`), com o cliente da API do frontend sendo gerado a partir desta especificação, garantindo consistência e confiabilidade.

## 3. Como Executar o Projeto

### Usando Docker (Recomendado)

A maneira mais simples de executar o projeto é utilizando Docker Compose.

1. **Escolha a configuração do banco de dados:** Existem arquivos `docker-compose` para diferentes bancos de dados e ambientes (desenvolvimento, produção). Por exemplo, para iniciar o ambiente de desenvolvimento com SQLite:
    ```bash
    docker-compose -f docker-compose.dev.sqlite.yml up --build
    ```
2. **Acesse a aplicação:** A aplicação web estará disponível em `http://localhost:8383`.

### Desenvolvimento Local

Para executar as aplicações individualmente:

- Utilize os scripts definidos no `package.json` da raiz, que são executados via `Turborepo`:
  ```bash
  # Inicia todas as aplicações em modo de desenvolvimento
  pnpm dev

  # Inicia apenas a API (backend)
  pnpm dev:api
  ```

## 4. Como Executar Testes

O projeto utiliza Playwright para testes end-to-end. Para executar a suíte de testes:

```bash
pnpm e2e
```

## 5. Principais Tecnologias

- **Backend:** Go
- **Frontend:** React, TypeScript, Vite, Tailwind CSS
- **Monorepo:** pnpm, Turborepo
- **Testes:** Playwright
- **Containerização:** Docker
- **CI/CD:** GitHub Actions

Este guia deve fornecer o contexto necessário para trabalhar neste repositório. Lembre-se de manter este documento atualizado à medida que o projeto evolui.
