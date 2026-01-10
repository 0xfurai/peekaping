package setting

import (
	"fmt"
	"net/http"
	"vigi/internal/utils"

	"regexp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var screamingSnakeCase = regexp.MustCompile(`^[A-Z0-9_]+$`)

type Controller struct {
	service Service
	logger  *zap.SugaredLogger
}

func NewController(
	service Service,
	logger *zap.SugaredLogger,
) *Controller {
	return &Controller{
		service,
		logger,
	}
}

// @Router	/settings/key/{key} [get]
// @Summary	Get setting by key
// @Tags		Settings
// @Produce	json
// @Security	JwtAuth
// @Security	ApiKeyAuth
// @Param	key	path	string	true	"Setting Key"
// @Success	200	{object}	utils.ApiResponse[Model]
// @Failure	404	{object}	utils.APIError[any]
// @Failure	500	{object}	utils.APIError[any]
func (ic *Controller) GetByKey(ctx *gin.Context) {
	key := ctx.Param("key")
	if !screamingSnakeCase.MatchString(key) {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Key must be SCREAMING_SNAKE_CASE (A-Z, 0-9, _)."))
		return
	}
	entity, err := ic.service.GetByKey(ctx, key)
	if err != nil {
		ic.logger.Errorw("Failed to fetch setting by key", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	if entity == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Setting not found"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", entity))
}

// @Router	/settings/key/{key} [put]
// @Summary	Set setting by key
// @Tags		Settings
// @Produce	json
// @Accept	json
// @Security	JwtAuth
// @Security	ApiKeyAuth
// @Param	key	path	string	true	"Setting Key"
// @Param	body	body	CreateUpdateDto	true	"Setting object"
// @Success	200	{object}	utils.ApiResponse[Model]
// @Failure	400	{object}	utils.APIError[any]
// @Failure	500	{object}	utils.APIError[any]
func (ic *Controller) SetByKey(ctx *gin.Context) {
	fmt.Println("SetByKey")
	key := ctx.Param("key")
	if !screamingSnakeCase.MatchString(key) {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Key must be SCREAMING_SNAKE_CASE (A-Z, 0-9, _)."))
		return
	}
	var entity CreateUpdateDto
	if err := ctx.ShouldBindJSON(&entity); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}
	if err := utils.Validate.Struct(entity); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}
	updated, err := ic.service.SetByKey(ctx, key, &entity)
	if err != nil {
		ic.logger.Errorw("Failed to set setting by key", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("setting set successfully", updated))
}

// @Router	/settings/key/{key} [delete]
// @Summary	Delete setting by key
// @Tags		Settings
// @Produce	json
// @Security	JwtAuth
// @Security	ApiKeyAuth
// @Param	key	path	string	true	"Setting Key"
// @Success	200	{object}	utils.ApiResponse[any]
// @Failure	404	{object}	utils.APIError[any]
// @Failure	500	{object}	utils.APIError[any]
func (ic *Controller) DeleteByKey(ctx *gin.Context) {
	key := ctx.Param("key")
	if !screamingSnakeCase.MatchString(key) {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Key must be SCREAMING_SNAKE_CASE (A-Z, 0-9, _)."))
		return
	}
	err := ic.service.DeleteByKey(ctx, key)
	if err != nil {
		ic.logger.Errorw("Failed to delete setting by key", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Setting deleted successfully", nil))
}
