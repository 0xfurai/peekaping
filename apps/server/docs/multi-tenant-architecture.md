### Documentação da Arquitetura Multi-usuário

#### 1. Visão Geral

O modelo proposto introduz uma arquitetura multi-tenant onde a **`Organization`** (Organização) é a unidade central de isolamento de dados. Cada recurso, como um `Monitor`, pertence a uma única organização. Um **`User`** (Usuário) pode ser membro de múltiplas organizações com diferentes níveis de permissão.

#### 2. Entidades Principais

*   **`User` (Usuário)**
    *   Representa uma pessoa física que se cadastra no sistema.
    *   A entidade `User` é independente e contém informações de autenticação (email, senha).
    *   Um usuário pode existir sem pertencer a nenhuma organização.

*   **`Organization` (Organização)**
    *   É o "container" principal para todos os recursos (monitores, alertas, etc.).
    *   Funciona como um tenant, garantindo que os dados de uma organização não sejam acessíveis por outra.

*   **`OrganizationUser` (Tabela de Vínculo)**
    *   Esta é a tabela de ligação que define a relação **muitos-para-muitos** entre `User` e `Organization`.
    *   Ela é a peça-chave do sistema de permissões. Cada entrada nesta tabela vincula um usuário a uma organização e especifica seu **`Role`** (papel) dentro dela.
    *   **Roles (Papéis):**
        *   `admin`: Tem controle total sobre a organização. Pode criar/editar/deletar monitores, convidar/remover membros e gerenciar as configurações da organização.
        *   `member`: Tem acesso limitado. Pode visualizar recursos (como monitores), mas não pode criar, editar ou deletar itens críticos, nem gerenciar membros.

*   **`Monitor` (e outros recursos)**
    *   Cada `Monitor` agora possui uma referência obrigatória (`OrgID`) que o vincula a uma `Organization`.
    *   Todo acesso a um monitor deve passar por uma verificação para garantir que o usuário pertence à organização proprietária do monitor.

#### 3. Fluxos de Usuário

1.  **Cadastro e Criação da Organização:**
    *   Um novo usuário se cadastra no sistema (cria uma entrada na tabela `User`).
    *   Ao criar sua primeira `Organization`, uma entrada é criada na tabela `Organization` e outra na tabela `OrganizationUser`, vinculando o `UserID` ao `OrgID` com o `Role` de **`admin`**.

2.  **Convite de Membros:**
    *   Um `admin` de uma organização pode convidar outros usuários (via email) para se juntarem à sua organização.
    *   Quando o usuário convidado aceita, uma nova entrada é criada na tabela `OrganizationUser` com o `UserID` do convidado, o `OrgID` da organização e o `Role` de **`member`** (ou outro papel definido pelo admin).

3.  **Acesso a Recursos (Ex: Monitores):**
    *   Quando um usuário tenta acessar um monitor, a lógica da aplicação deve:
        1.  Verificar se o usuário está autenticado.
        2.  Obter o `OrgID` do monitor que ele está tentando acessar.
        3.  Consultar a tabela `OrganizationUser` para verificar se existe uma entrada com o `UserID` do usuário e o `OrgID` do recurso.
        4.  Se a entrada existir, a ação é permitida (e pode haver verificações adicionais com base no `Role`).
        5.  Se não existir, o acesso é negado com um erro de "Não autorizado".

### Diagrama da Arquitetura (Modelo Entidade-Relacionamento)

```mermaid
erDiagram
    User {
        string UserID PK "Chave Primária"
        string Name
        string Email
        string PasswordHash
    }

    Organization {
        string OrgID PK "Chave Primária"
        string Name
    }

    OrganizationUser {
        string UserID FK "Chave Estrangeira (User)"
        string OrgID FK "Chave Estrangeira (Organization)"
        string Role "enum: 'admin', 'member'"
    }

    Monitor {
        string MonitorID PK "Chave Primária"
        string OrgID FK "Chave Estrangeira (Organization)"
        string Name
        string Type
        -- outros campos
    }

    User ||--|{ OrganizationUser : "has role in"
    Organization ||--|{ OrganizationUser : "has"
    Organization ||--|{ Monitor : "owns"
```