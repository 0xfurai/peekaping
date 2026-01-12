package organization

type CreateOrganizationDto struct {
	Name string `json:"name" validate:"required,min=3" example:"My Organization"`
	Slug string `json:"slug" validate:"omitempty,min=3" example:"my-organization"`
}

type UpdateOrganizationDto struct {
	Name *string `json:"name" validate:"min=3" example:"Updated Organization Name"`
	Slug *string `json:"slug" validate:"omitempty,min=3" example:"updated-slug"`
}

type AddMemberDto struct {
	Email string `json:"email" validate:"required,email" example:"user@example.com"`
	Role  Role   `json:"role" validate:"required,oneof=admin member" example:"member"`
}

type UpdateMemberRoleDto struct {
	Role Role `json:"role" validate:"required,oneof=admin member" example:"admin"`
}

type OrganizationResponseDto struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type OrganizationMemberResponseDto struct {
	UserID           string           `json:"user_id"`
	Role             Role             `json:"role"`
	JoinedAt         string           `json:"joined_at"`
	OrganizationName string           `json:"organization_name,omitempty"`
	User             *UserResponseDto `json:"user,omitempty"`
	Status           string           `json:"status"`                     // "active" or "pending"
	InvitationToken  string           `json:"invitation_token,omitempty"` // Only for pending
}

type UserResponseDto struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"` // Will be empty or email prefix for now
}
