package organization

type CreateOrganizationDto struct {
	Name string `json:"name" validate:"required,min=3" example:"My Organization"`
}

type UpdateOrganizationDto struct {
	Name *string `json:"name" validate:"min=3" example:"Updated Organization Name"`
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
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type OrganizationMemberResponseDto struct {
	UserID   string `json:"user_id"`
	Role     Role   `json:"role"`
	JoinedAt string `json:"joined_at"`
}
