package tag

type CreateUpdateDto struct {
	Name        string  `json:"name" validate:"required,min=1,max=100" example:"Production"`
	Color       string  `json:"color" validate:"required,hexcolor" example:"#3B82F6"`
	Description *string `json:"description" example:"Production environment monitors"`
}

type PartialUpdateDto struct {
	Name        *string `json:"name,omitempty" validate:"omitempty,min=1,max=100" example:"Production"`
	Color       *string `json:"color,omitempty" validate:"omitempty,hexcolor" example:"#3B82F6"`
	Description *string `json:"description,omitempty" example:"Production environment monitors"`
}
