package handler

import (
	"teach_me_all/internal/repository"

	"github.com/gofiber/fiber/v3"
)

type CourseHandler struct{
	repo repository.CourseRepository
}


func NewCourseHandler(repo repository.CourseRepository) *CourseHandler{
	return &CourseHandler{repo:repo}
}


func (h *CourseHandler) GetByCourseID(c fiber.Ctx) error{
	userId := c.Params("userId")
	
	result,err:=h.repo.GetByCourseID(c.Context(),userId)
	if err!=nil{
		return  fiber.NewError(fiber.StatusInternalServerError,err.Error())
	}

	return c.JSON(result)
	
}
// func (h *CourseHandler) List(c fiber.Ctx) error{

// }

