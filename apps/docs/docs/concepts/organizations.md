---
sidebar_position: 1
---

# Organizations & Multi-tenancy

Vigi uses a multi-tenant architecture centered around **Organizations**. This ensures strict data isolation and security between different teams or workspaces.

## Structure

- **Organization**: The top-level entity. All resources (Monitors, Status Pages, Notification Channels, Settings) belong to a specific organization.
- **User**: Users are global entities but can be members of multiple organizations.
- **Membership**: A user's access to an organization is defined by their membership role.

## Data Isolation

All data queries and mutations are scoped to the `Organization ID` of the active session. This guarantees that:
- Users can only access resources within the organizations they are members of.
- Resources from one organization are completely invisible to others.

## Invitations Flow

Vigi supports a secure invitation flow for adding new members:

1.  **Invite**: An organization admin sends an invite using the new member's email.
2.  **Token**: A unique, secure token is generated and sent via email (or provided as a link).
3.  **Accept**: The invited user, upon logging in or creating an account, can accept the invitation.
4.  **Join**: Once accepted, the user becomes a member and gains immediate access to the organization's dashboard.

### API Usage

You can manage invitations and organizations programmatically via the API. See the [API Reference](/api/vigi-api) for details on endpoints like:
- `POST /invitations/accept/{token}`
- `GET /account/invitations`
- `POST /organizations/{orgSlug}/members`
