# Documentação do Fluxo de Autenticação do Backend

Este documento detalha o funcionamento interno do sistema de autenticação no backend, localizado em `apps/server`.

## Visão Geral

O sistema de autenticação é construído em torno de tokens JWT (JSON Web Tokens) e foi projetado com um modelo de **administrador único**. Ele lida com registro, login, gerenciamento de sessão e segurança.

## 1. Registro de Conta

O fluxo de registro é intencionalmente restritivo para garantir que apenas um administrador possa gerenciar o sistema.

- **Administrador Único**: O sistema permite o registro de apenas **um usuário**. A primeira conta criada com sucesso se torna a administradora permanente.
- **Bloqueio de Novos Registros**: Após o primeiro registro, qualquer tentativa subsequente de criar uma nova conta é bloqueada. Isso é verificado na função `Register` em [`auth.service.go`](./internal/modules/auth/auth.service.go).
- **Hashing de Senha**: A senha do usuário é criptografada usando o algoritmo **bcrypt**, uma prática de segurança robusta que previne ataques de rainbow table.

## 2. Login e Autenticação

O processo de login valida as credenciais do usuário e, se habilitado, a autenticação de dois fatores.

- **Validação de Credenciais**: O login é feito através do endpoint `/auth/login`. O sistema compara a senha fornecida com o hash armazenado no banco de dados.
- **Autenticação de Dois Fatores (2FA)**: Se o 2FA (baseado em TOTP) estiver ativado para a conta, o usuário deve fornecer um código de 6 dígitos válido para concluir o login. A lógica de validação está em [`auth.service.go`](./internal/modules/auth/auth.service.go).
- **Proteção contra Brute-Force**: A rota de login é protegida por um *rate limiter* para mitigar ataques de força bruta, implementado em [`auth.route.go`](./internal/modules/auth/auth.route.go).

## 3. Gerenciamento de Sessão com JWT

A sessão do usuário é gerenciada por meio de `access` e `refresh tokens`.

- **Access Token**: Um token de curta duração que concede acesso aos endpoints protegidos da API. Ele é enviado no cabeçalho `Authorization` de cada requisição.
- **Refresh Token**: Um token de longa duração usado para gerar um novo `access token` quando o atual expira. Isso permite que o usuário permaneça logado por mais tempo sem precisar reinserir suas credenciais.
- **Geração e Validação**: A lógica para criar e verificar os tokens JWT está centralizada em [`jwt.go`](./internal/modules/auth/jwt.go). As chaves secretas e os tempos de expiração são configuráveis, proporcionando flexibilidade e segurança.

## 4. Estrutura de Arquivos Relevantes

- **[`internal/modules/auth/auth.route.go`](./internal/modules/auth/auth.route.go)**: Define as rotas da API para `register`, `login`, `refresh`, `2fa` e `password update`.
- **[`internal/modules/auth/auth.controller.go`](./internal/modules/auth/auth.controller.go)**: Lida com as requisições HTTP, valida os dados de entrada (DTOs) e chama os serviços correspondentes.
- **[`internal/modules/auth/auth.service.go`](./internal/modules/auth/auth.service.go)**: Contém a lógica de negócio principal para todas as operações de autenticação.
- **[`internal/modules/auth/jwt.go`](./internal/modules/auth/jwt.go)**: Responsável por toda a lógica de criação e verificação dos JSON Web Tokens.
- **[`internal/modules/auth/auth.model.go`](./internal/modules/auth/auth.model.go)**: Define a estrutura de dados do usuário (`Model`) armazenada no banco de dados.
