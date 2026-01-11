# Documentação do Fluxo de Autenticação do Frontend

Este documento detalha o funcionamento do sistema de autenticação no frontend, localizado em `apps/web`.

## Visão Geral

O frontend é construído com React e Vite e é responsável por gerenciar a sessão do usuário, proteger o acesso às páginas e interagir com a API de autenticação do backend.

## 1. Gerenciamento de Estado com Zustand

O estado de autenticação do usuário é gerenciado de forma centralizada usando a biblioteca **Zustand**.

- **`useAuthStore`**: O estado global de autenticação é mantido no *store* `useAuthStore`, definido em [`src/store/auth.ts`](./src/store/auth.ts). Este *store* armazena:
  - `accessToken`: O token JWT para autorizar requisições à API.
  - `refreshToken`: O token para renovar a sessão do usuário.
  - `user`: As informações do usuário logado.
- **Persistência de Dados**: O `useAuthStore` utiliza o middleware `persist`, que salva automaticamente o estado de autenticação no **`localStorage`** do navegador. Isso garante que o usuário permaneça logado mesmo após recarregar a página ou fechar o navegador, proporcionando uma experiência de usuário contínua.
- **Ações**: O *store* expõe ações para `setTokens`, `setUser`, e `clearTokens`, permitindo que os componentes atualizem o estado de autenticação de forma segura.

## 2. Proteção de Rotas

O acesso às diferentes partes da aplicação é controlado com base no estado de autenticação do usuário.

- **`AppRouter`**: O componente [`src/components/app-router.tsx`](./src/components/app-router.tsx) é o coração do sistema de roteamento. Ele lê o `accessToken` do `useAuthStore` para decidir quais rotas renderizar.
- **Rotas Públicas e Protegidas**:
  - **Se um `accessToken` existe**: O usuário é considerado autenticado, e o `AppRouter` renderiza as rotas protegidas definidas em [`src/routes/protected-routes.tsx`](./src/routes/protected-routes.tsx).
  - **Se não há `accessToken`**: O usuário é redirecionado para as rotas de autenticação (login/registro), definidas em [`src/routes/auth-routes.tsx`](./src/routes/auth-routes.tsx).
- **Redirecionamento Automático**: Essa abordagem garante que um usuário não autenticado não possa acessar páginas internas da aplicação diretamente pela URL.

## 3. Comunicação com a API

A interação com o backend é feita por meio de um cliente de API gerado automaticamente.

- **SDK Gerado**: O cliente da API, localizado em [`src/api/`](./src/api/), é gerado a partir da especificação OpenAPI (Swagger) do backend. Isso garante que as chamadas de função, os tipos de dados e os endpoints estejam sempre sincronizados entre o frontend e o backend.
- **Interceptadores de Requisição**: Para enviar o token de autenticação em cada requisição, um interceptador (como no arquivo `interceptors.ts`) é usado para adicionar automaticamente o `accessToken` do `useAuthStore` ao cabeçalho `Authorization` de todas as chamadas para a API.

## 4. Estrutura de Arquivos Relevantes

- **[`src/store/auth.ts`](./src/store/auth.ts)**: Define o *store* do Zustand para o estado de autenticação.
- **[`src/components/app-router.tsx`](./src/components/app-router.tsx)**: Componente principal que implementa a lógica de roteamento e proteção de rotas.
- **[`src/routes/protected-routes.tsx`](./src/routes/protected-routes.tsx)**: Define o conjunto de rotas que exigem autenticação.
- **[`src/routes/auth-routes.tsx`](./src/routes/auth-routes.tsx)**: Define as rotas de login e registro.
- **[`src/api/`](./src/api/)**: Contém o SDK da API gerado para a comunicação com o backend.
